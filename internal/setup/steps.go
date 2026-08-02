package setup

import (
	"os"
	"path/filepath"

	"github.com/jrdriscoll17/hydra/internal/sys"
	"github.com/jrdriscoll17/hydra/internal/theme"
)

// Post-install bootstrap: the things no package manager and no config file
// tracks. Each step pairs a check with an action, so re-running installs only
// what is genuinely absent.

const wallpapersRepo = "git@github.com:jrdriscoll17/wallpapers.git"

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

func wallpapersLinked() bool {
	entries, err := os.ReadDir(sys.InHome(".config/hypr/wallpapers"))
	return err == nil && len(entries) > 0
}

func linkWallpapers() error {
	dst := sys.InHome("wallpapers")
	if !sys.Exists(dst) {
		if err := sys.Run("git", "clone", wallpapersRepo, dst); err != nil {
			return err
		}
	}
	link := sys.InHome(".config/hypr/wallpapers")
	if sys.Exists(link) {
		os.Remove(link)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(dst, link)
}
