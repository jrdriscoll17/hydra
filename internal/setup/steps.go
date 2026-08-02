package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
	"github.com/jrdriscoll17/hydra/internal/theme"
)

// Post-install bootstrap: the things no package manager and no config file
// tracks. Each step pairs a check with an action, so re-running installs only
// what is genuinely absent.

const wallpapersRepo = "git@github.com:jrdriscoll17/wallpapers.git"

// httpsURL rewrites an SSH remote to its HTTPS equivalent:
// git@github.com:user/repo.git -> https://github.com/user/repo.git
// Returns "" for anything that is not an scp-style SSH URL.
func httpsURL(remote string) string {
	rest, ok := strings.CutPrefix(remote, "git@")
	if !ok {
		return ""
	}
	host, path, ok := strings.Cut(rest, ":")
	if !ok || host == "" || path == "" {
		return ""
	}
	return "https://" + host + "/" + path
}

// cloneWithFallback clones over SSH, then over HTTPS if that fails.
//
// A machine set up from scratch usually has no GitHub key yet, and these repos
// are public — so falling back means the wallpapers arrive anyway instead of
// the step failing and the desktop coming up bare. The SSH remote is tried
// first so a machine that does have a key keeps a pushable remote.
func cloneWithFallback(remote, dst string) error {
	err := sys.Run("git", "clone", remote, dst)
	if err == nil {
		return nil
	}

	alt := httpsURL(remote)
	if alt == "" {
		return err
	}
	fmt.Println(dimStyle.Render("    ssh clone failed, retrying over https..."))
	// A failed clone can leave a partial directory behind, which would make
	// the retry fail for the wrong reason.
	os.RemoveAll(dst)
	return sys.Run("git", "clone", alt, dst)
}

func tpmInstalled() bool { return sys.Exists(sys.InHome(".tmux/plugins/tpm")) }

func installTPM() error {
	return sys.Run("git", "clone", "--depth", "1",
		"https://github.com/tmux-plugins/tpm", sys.InHome(".tmux/plugins/tpm"))
}

func fisherInstalled() bool {
	out, err := sys.Capture("fish", "-c", "fisher list")
	return err == nil && out != ""
}

func installFisher() error {
	// fisher.fish ships in the repo, so the function exists once configs are
	// applied; `fisher update` then installs everything in fish_plugins.
	return sys.Run("fish", "-c", "fisher update")
}

func lazySynced() bool { return sys.Exists(sys.InHome(".local/share/nvim/lazy/lazy.nvim")) }

func syncLazy() error {
	return sys.Run("nvim", "--headless", "+Lazy! sync", "+qa")
}

func doomInstalled() bool { return sys.Exists(sys.InHome(".config/emacs/bin/doom")) }

func installDoom() error {
	if !sys.Exists(sys.InHome(".config/emacs")) {
		err := sys.Run("git", "clone", "--depth", "1",
			"https://github.com/doomemacs/doomemacs", sys.InHome(".config/emacs"))
		if err != nil {
			return err
		}
	}
	return sys.Run(sys.InHome(".config/emacs/bin/doom"), "install", "--no-config", "--force")
}

// themeRendered reports whether the theme output on disk is both present and
// produced by this build of hydra.
//
// The presence half is kitty's generated include, whose absence makes kitty
// error on startup. The build half is what makes an upgrade self-healing: change
// a renderer, and the next `hydra` or `hydra sync` re-renders on its own rather
// than trusting output an older binary wrote.
func themeRendered() bool {
	return sys.Exists(sys.InHome(".config/kitty/theme.conf")) && renderIsCurrent()
}

func applyTheme() error {
	if err := theme.Apply(theme.Current(), true); err != nil {
		return err
	}
	return saveRenderStamp()
}

// hasWallpapers reports whether a directory actually holds wallpaper images.
//
// Counting entries is not enough. A clone that fails part-way leaves a lone
// .git behind, and through the symlink that reads as a populated directory —
// so the step reported itself done, `hydra status` said "in sync", and the
// desktop still came up with no wallpaper and nothing to explain why.
func hasWallpapers(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".jpg", ".jpeg", ".png", ".webp", ".bmp":
			return true
		}
	}
	return false
}

// salvageable reports whether a directory is empty or holds nothing but .git,
// which is what a failed clone leaves. Anything else might be the user's own
// files and is not this tool's to delete.
func salvageable(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			return false
		}
	}
	return true
}

// missingWallpapers lists the wallpapers the palettes name that are not on
// disk.
//
// "Is there any image here?" was the wrong question, in the same way that
// counting directory entries was before it: a checkout missing the one file the
// live palette points at passed the check, reported itself done, and left the
// desktop with an empty hyprpaper.conf. What matters is the specific files the
// palettes ask for.
func missingWallpapers() []string {
	dir := sys.InHome(".config/hypr/wallpapers")
	var missing []string
	for _, p := range palettes() {
		if p.Wallpaper == "" {
			continue
		}
		if !sys.Exists(filepath.Join(dir, p.Wallpaper)) {
			missing = append(missing, p.Wallpaper)
		}
	}
	return missing
}

func wallpapersLinked() bool {
	// Before the configs are applied there are no palettes to ask about, so
	// fall back to whether anything is there at all — otherwise a fresh machine
	// would report this done and never clone.
	if len(palettes()) == 0 {
		return hasWallpapers(sys.InHome(".config/hypr/wallpapers"))
	}
	return len(missingWallpapers()) == 0
}

func linkWallpapers() error {
	dst := sys.InHome("wallpapers")

	// An existing checkout that is merely behind is worth a pull before
	// anything more drastic — the palettes may name a wallpaper added after
	// this machine cloned. Best effort: a failure here just falls through to
	// the checks below.
	if sys.Exists(filepath.Join(dst, ".git")) && len(missingWallpapers()) > 0 {
		fmt.Println(dimStyle.Render("    wallpapers missing, pulling..."))
		_ = sys.Run("git", "-C", dst, "pull", "--ff-only")
	}

	if !hasWallpapers(dst) {
		// Only the wreckage of a previous attempt gets cleared; a directory
		// with real content in it is left alone and reported.
		if sys.Exists(dst) {
			if !salvageable(dst) {
				return fmt.Errorf("%s exists but holds no wallpapers; "+
					"move it aside and re-run", dst)
			}
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
		}
		if err := cloneWithFallback(wallpapersRepo, dst); err != nil {
			return err
		}
		if !hasWallpapers(dst) {
			return fmt.Errorf("cloned %s but found no wallpapers in %s",
				wallpapersRepo, dst)
		}
	}

	link := sys.InHome(".config/hypr/wallpapers")
	if fi, err := os.Lstat(link); err == nil {
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			// Replacing a link is routine.
			if err := os.Remove(link); err != nil {
				return err
			}
		case fi.IsDir() && salvageable(link):
			// An empty directory gets left here by hyprpaper, or by an earlier
			// run that failed between creating it and linking. There is nothing
			// in it to lose, and refusing to clear it only strands the machine
			// with no wallpapers and a message telling the user to do it by
			// hand.
			if err := os.RemoveAll(link); err != nil {
				return err
			}
		case fi.IsDir():
			// A directory with real content in it, though, is someone's own
			// wallpapers and is not ours to delete.
			return fmt.Errorf("%s is a directory with files in it, not a link "+
				"into %s; move it aside and re-run", link, dst)
		default:
			if err := os.Remove(link); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(dst, link); err != nil {
		return err
	}

	// Name the files that are still absent rather than leaving the desktop to
	// come up bare with nothing said. A palette can point at a wallpaper that
	// was never committed, which no amount of re-cloning will fix.
	if gone := missingWallpapers(); len(gone) > 0 {
		return fmt.Errorf("still missing from %s: %s (a palette names it, but "+
			"%s does not have it)", dst, strings.Join(gone, ", "), wallpapersRepo)
	}
	return nil
}
