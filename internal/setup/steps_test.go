package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bootstrap checks all answer "is this actually on the machine?", never
// "have I run before". Each one is a plain filesystem probe, so they are
// testable against a temporary HOME.

func TestBootstrapChecksProbeRealState(t *testing.T) {
	cases := []struct {
		name  string
		check func() bool
		// path, relative to HOME, whose presence should flip the check.
		path string
		dir  bool
	}{
		{name: "tpm", check: tpmInstalled, path: ".tmux/plugins/tpm", dir: true},
		{name: "lazy.nvim", check: lazySynced, path: ".local/share/nvim/lazy/lazy.nvim", dir: true},
		{name: "doom", check: doomInstalled, path: ".config/emacs/bin/doom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			if c.check() {
				t.Fatal("check reported installed on an empty HOME")
			}
			if c.dir {
				mkdir(t, filepath.Join(home, c.path))
			} else {
				writeFile(t, filepath.Join(home, c.path), "#!/bin/sh\n")
			}
			if !c.check() {
				t.Errorf("check reported missing after creating %s", c.path)
			}
		})
	}
}

func TestWallpapersLinked(t *testing.T) {
	t.Run("no wallpapers directory", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if wallpapersLinked() {
			t.Error("wallpapersLinked = true on an empty HOME")
		}
	})

	// An empty directory means the clone failed or the symlink points nowhere
	// useful, which is not "done".
	t.Run("empty directory does not count", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		mkdir(t, filepath.Join(home, ".config/hypr/wallpapers"))

		if wallpapersLinked() {
			t.Error("wallpapersLinked = true for an empty directory")
		}
	})

	t.Run("a populated symlink counts", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		repo := filepath.Join(home, "wallpapers")
		writeFile(t, filepath.Join(repo, "forest.jpg"), "jpeg")
		mkdir(t, filepath.Join(home, ".config/hypr"))
		if err := os.Symlink(repo, filepath.Join(home, ".config/hypr/wallpapers")); err != nil {
			t.Fatal(err)
		}

		if !wallpapersLinked() {
			t.Error("wallpapersLinked = false for a populated symlink")
		}
	})

	t.Run("a dangling symlink does not count", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		mkdir(t, filepath.Join(home, ".config/hypr"))
		if err := os.Symlink(filepath.Join(home, "nowhere"),
			filepath.Join(home, ".config/hypr/wallpapers")); err != nil {
			t.Fatal(err)
		}

		if wallpapersLinked() {
			t.Error("wallpapersLinked = true for a dangling symlink")
		}
	})
}

func TestLinkWallpapers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pre-create the clone target so linkWallpapers skips the git clone.
	repo := filepath.Join(home, "wallpapers")
	writeFile(t, filepath.Join(repo, "forest.jpg"), "jpeg")

	if err := linkWallpapers(); err != nil {
		t.Fatalf("linkWallpapers: %v", err)
	}

	link := filepath.Join(home, ".config/hypr/wallpapers")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("not a symlink: %v", err)
	}
	if target != repo {
		t.Errorf("link -> %q, want %q", target, repo)
	}
	if !wallpapersLinked() {
		t.Error("wallpapersLinked = false after linkWallpapers")
	}
}

func TestLinkWallpapersReplacesAStaleLink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := filepath.Join(home, "wallpapers")
	writeFile(t, filepath.Join(repo, "forest.jpg"), "jpeg")

	link := filepath.Join(home, ".config/hypr/wallpapers")
	mkdir(t, filepath.Dir(link))
	if err := os.Symlink(filepath.Join(home, "somewhere-else"), link); err != nil {
		t.Fatal(err)
	}

	if err := linkWallpapers(); err != nil {
		t.Fatalf("linkWallpapers: %v", err)
	}
	if target, _ := os.Readlink(link); target != repo {
		t.Errorf("stale link not replaced: -> %q, want %q", target, repo)
	}
}

// -- themeRendered -----------------------------------------------------------

// The two halves matter for different reasons: the file's absence makes kitty
// fail to start, and a stale stamp is what makes an upgrade self-healing.
func TestThemeRendered(t *testing.T) {
	if selfStamp() == "" {
		t.Skip("cannot fingerprint this binary")
	}

	newMachine := func(t *testing.T) (home, state string) {
		t.Helper()
		home = t.TempDir()
		t.Setenv("HOME", home)
		state = useStateDir(t)
		return home, state
	}

	t.Run("nothing rendered yet", func(t *testing.T) {
		newMachine(t)
		if themeRendered() {
			t.Error("themeRendered = true on a fresh machine")
		}
	})

	// The regression this guards: checking only that theme.conf exists reports
	// "already done" after a hydra upgrade that changed a renderer, and the
	// machine keeps output an older binary wrote.
	t.Run("output present but written by another build", func(t *testing.T) {
		home, state := newMachine(t)
		writeFile(t, filepath.Join(home, ".config/kitty/theme.conf"), "# generated")
		writeFile(t, filepath.Join(state, "render-stamp"), strings.Repeat("b", 64)+"\n")

		if themeRendered() {
			t.Error("themeRendered = true for output written by a different build; " +
				"an upgrade would never re-render")
		}
	})

	t.Run("output present and current", func(t *testing.T) {
		home, _ := newMachine(t)
		writeFile(t, filepath.Join(home, ".config/kitty/theme.conf"), "# generated")
		if err := saveRenderStamp(); err != nil {
			t.Fatal(err)
		}
		if !themeRendered() {
			t.Error("themeRendered = false for current output")
		}
	})

	// kitty.conf has `include theme.conf` and errors outright without it, so a
	// current stamp is not enough on its own.
	t.Run("stamp current but the output is gone", func(t *testing.T) {
		newMachine(t)
		if err := saveRenderStamp(); err != nil {
			t.Fatal(err)
		}
		if themeRendered() {
			t.Error("themeRendered = true with no theme.conf on disk")
		}
	})
}

func TestHTTPSURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"git@github.com:jrdriscoll17/wallpapers.git", "https://github.com/jrdriscoll17/wallpapers.git"},
		{"git@github.com:jrdriscoll17/dotfiles.git", "https://github.com/jrdriscoll17/dotfiles.git"},
		{"git@gitlab.com:group/proj.git", "https://gitlab.com/group/proj.git"},
		// Already HTTPS, or not an scp-style URL: nothing to rewrite.
		{"https://github.com/x/y.git", ""},
		{"git@github.com", ""},
		{"git@:no-host.git", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := httpsURL(c.in); got != c.want {
			t.Errorf("httpsURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The repos are public, so a machine with no GitHub key still gets its
// wallpapers rather than the step failing and the desktop coming up bare.
func TestCloneWithFallbackUsesHTTPSWhenSSHFails(t *testing.T) {
	dir := t.TempDir()
	// A git that refuses ssh remotes and "clones" https ones by making the dir.
	script := "#!/bin/sh\n" +
		"# args: clone <remote> <dst>\n" +
		"case \"$2\" in\n" +
		"  git@*) echo 'Permission denied (publickey).' >&2; mkdir -p \"$3\"; exit 128 ;;\n" +
		"  https://*) mkdir -p \"$3\" && echo cloned > \"$3/marker\" ; exit 0 ;;\n" +
		"esac\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dst := filepath.Join(t.TempDir(), "wallpapers")
	if err := cloneWithFallback("git@github.com:jrdriscoll17/wallpapers.git", dst); err != nil {
		t.Fatalf("cloneWithFallback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "marker")); err != nil {
		t.Errorf("the https fallback did not run: %v", err)
	}
}

// A failed ssh clone can leave a partial directory behind; the retry must not
// trip over it.
func TestCloneWithFallbackClearsAPartialClone(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$2\" in\n" +
		"  git@*) mkdir -p \"$3\"; touch \"$3/partial\"; exit 128 ;;\n" +
		"  https://*) [ -e \"$3\" ] && { echo 'dst exists' >&2; exit 128; }; " +
		"mkdir -p \"$3\"; echo ok > \"$3/marker\"; exit 0 ;;\n" +
		"esac\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dst := filepath.Join(t.TempDir(), "wallpapers")
	if err := cloneWithFallback("git@github.com:x/y.git", dst); err != nil {
		t.Fatalf("cloneWithFallback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "partial")); err == nil {
		t.Error("the partial ssh clone survived into the retry")
	}
}

// -- wallpaper presence ------------------------------------------------------

// The bug this pins: a clone that failed part-way leaves a lone .git behind,
// and counting directory entries read that as success. `hydra status` then
// reported "in sync" while the desktop had no wallpaper and nothing said why.
func TestHasWallpapers(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		dirs  []string
		want  bool
	}{
		{name: "real wallpapers", files: []string{"forest_mist.jpg"}, want: true},
		{name: "png", files: []string{"a.png"}, want: true},
		{name: "mixed case extension", files: []string{"A.JPG"}, want: true},
		{name: "empty", want: false},
		{name: "only a .git directory", dirs: []string{".git"}, want: false},
		{name: "git plus a readme", files: []string{"README.md"}, dirs: []string{".git"}, want: false},
		// The images live at the top level; a subdirectory of them is the
		// repo's `original/` originals, not what the palettes point at.
		{name: "images only in a subdirectory", dirs: []string{"original"}, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range c.dirs {
				mkdir(t, filepath.Join(dir, d))
			}
			for _, f := range c.files {
				writeFile(t, filepath.Join(dir, f), "x")
			}
			if got := hasWallpapers(dir); got != c.want {
				t.Errorf("hasWallpapers = %v, want %v", got, c.want)
			}
		})
	}

	if hasWallpapers(filepath.Join(t.TempDir(), "missing")) {
		t.Error("hasWallpapers = true for a directory that does not exist")
	}
}

func TestSalvageable(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if !salvageable(t.TempDir()) {
			t.Error("an empty directory should be salvageable")
		}
	})

	t.Run("only .git", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, filepath.Join(dir, ".git"))
		if !salvageable(dir) {
			t.Error("a bare .git is the wreckage of a failed clone and should be salvageable")
		}
	})

	// Anything else might be the user's own files.
	t.Run("has other content", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "mine.txt"), "x")
		if salvageable(dir) {
			t.Error("a directory with real content must not be cleared")
		}
	})
}

// A wallpapers directory holding only a failed clone's .git must report the
// step as still pending, not as done.
func TestWallpapersLinkedRejectsAPartialClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := filepath.Join(home, "wallpapers")
	mkdir(t, filepath.Join(repo, ".git"))
	mkdir(t, filepath.Join(home, ".config/hypr"))
	if err := os.Symlink(repo, filepath.Join(home, ".config/hypr/wallpapers")); err != nil {
		t.Fatal(err)
	}

	if wallpapersLinked() {
		t.Error("wallpapersLinked = true for a clone that left only .git — " +
			"this is what made `hydra status` report in sync with no wallpapers")
	}
}

// A real directory at the link path is someone's own wallpapers; refuse rather
// than delete it. os.Remove would fail on it anyway, and that failure was
// previously discarded, leaving a confusing EEXIST from the symlink.
// An empty directory is left behind by hyprpaper, or by an earlier run that
// died between creating it and linking. Refusing to clear it stranded the
// machine with no wallpapers and a message telling the user to fix it by hand.
func TestLinkWallpapersClearsAnEmptyRealDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, "wallpapers", "new_rock.jpg"), "jpeg")
	mkdir(t, filepath.Join(home, ".config/hypr/wallpapers"))

	if err := linkWallpapers(); err != nil {
		t.Fatalf("linkWallpapers: %v", err)
	}
	target, err := os.Readlink(filepath.Join(home, ".config/hypr/wallpapers"))
	if err != nil {
		t.Fatalf("an empty directory was not replaced with a link: %v", err)
	}
	if want := filepath.Join(home, "wallpapers"); target != want {
		t.Errorf("link -> %q, want %q", target, want)
	}
}

// A directory with files in it is not ours to delete — but stopping and
// telling the user to move it by hand leaves the machine broken for no good
// reason. Back it up and carry on, which is what the conflict prompt does with
// a config file that is in the way.
func TestLinkWallpapersMovesARealDirectoryAside(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, "wallpapers", "new_rock.jpg"), "jpeg")
	writeFile(t, filepath.Join(home, ".config/hypr/wallpapers", "mine.jpg"), "not from the repo")

	out := quiet(t, func() {
		if err := linkWallpapers(); err != nil {
			t.Errorf("linkWallpapers: %v", err)
		}
	})

	link := filepath.Join(home, ".config/hypr/wallpapers")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("the link was not created: %v", err)
	}
	if want := filepath.Join(home, "wallpapers"); target != want {
		t.Errorf("link -> %q, want %q", target, want)
	}

	// Nothing may be lost.
	kept := readFile(t, link+".before-setup/mine.jpg")
	if kept != "not from the repo" {
		t.Errorf("the displaced files were not preserved: %q", kept)
	}
	if !strings.Contains(out, "before-setup") {
		t.Errorf("the move was not reported:\n%s", out)
	}
}

// A second run must not destroy what the first preserved.
func TestMoveAsideNeverOverwritesAnExistingBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "wallpapers")

	mkdir(t, target)
	writeFile(t, filepath.Join(target, "first.jpg"), "first")
	if _, err := moveAside(target); err != nil {
		t.Fatal(err)
	}

	mkdir(t, target)
	writeFile(t, filepath.Join(target, "second.jpg"), "second")
	second, err := moveAside(target)
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(dir, "wallpapers.before-setup/first.jpg")); got != "first" {
		t.Errorf("the first backup was clobbered: %q", got)
	}
	if got := readFile(t, filepath.Join(second, "second.jpg")); got != "second" {
		t.Errorf("the second backup is wrong: %q", got)
	}
}

func TestLinkWallpapersClearsAFailedCloneAndRetries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The wreckage of an earlier failed clone.
	mkdir(t, filepath.Join(home, "wallpapers", ".git"))

	dir := t.TempDir()
	script := "#!/bin/sh\nmkdir -p \"$3\" && echo jpeg > \"$3/forest.jpg\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := linkWallpapers(); err != nil {
		t.Fatalf("linkWallpapers: %v", err)
	}
	if !wallpapersLinked() {
		t.Error("wallpapersLinked = false after a successful retry")
	}
}

// -- the wallpapers the palettes actually name -------------------------------

// "Is there any image here?" was the wrong question. A checkout missing the one
// file the live palette points at passed the check, reported itself done, and
// left hyprpaper with nothing to show — while `hydra status` said in sync.
func TestWallpapersLinkedChecksTheNamedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")

	dir := filepath.Join(home, ".config/hypr/wallpapers")

	// Some other wallpaper is present, but not the one the palette names.
	writeFile(t, filepath.Join(dir, "something_else.jpg"), "jpeg")
	if wallpapersLinked() {
		t.Error("wallpapersLinked = true while the palette's own wallpaper is absent — " +
			"this is what reported done with an empty hyprpaper.conf")
	}
	if got := missingWallpapers(); len(got) != 1 || got[0] != "w.jpg" {
		t.Errorf("missingWallpapers = %v, want [w.jpg]", got)
	}

	// Now the named one arrives.
	writeFile(t, filepath.Join(dir, "w.jpg"), "jpeg")
	if !wallpapersLinked() {
		t.Error("wallpapersLinked = false with every named wallpaper present")
	}
	if got := missingWallpapers(); len(got) != 0 {
		t.Errorf("missingWallpapers = %v, want none", got)
	}
}

// Every palette's wallpaper counts, not just the live one — switching theme
// must not land on a missing file.
func TestMissingWallpapersCoversEveryPalette(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".config/theme/themes/a.json"), paletteWith("a", "one.jpg"))
	writeFile(t, filepath.Join(home, ".config/theme/themes/b.json"), paletteWith("b", "two.jpg"))
	writeFile(t, filepath.Join(home, ".config/hypr/wallpapers/one.jpg"), "jpeg")

	got := missingWallpapers()
	if len(got) != 1 || got[0] != "two.jpg" {
		t.Errorf("missingWallpapers = %v, want [two.jpg]", got)
	}
	if wallpapersLinked() {
		t.Error("wallpapersLinked = true while another palette's wallpaper is absent")
	}
}

// On a machine where the configs have not landed yet there are no palettes to
// ask about, so the check must not report done and skip the clone.
func TestWallpapersLinkedOnAFreshMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if wallpapersLinked() {
		t.Error("wallpapersLinked = true on a machine with no palettes and no wallpapers")
	}

	writeFile(t, filepath.Join(home, ".config/hypr/wallpapers/anything.jpg"), "jpeg")
	if !wallpapersLinked() {
		t.Error("wallpapersLinked = false with wallpapers present and no palettes to check against")
	}
}

func paletteWith(name, wallpaper string) string {
	return `{
  "name": "` + name + `",
  "label": "` + name + `",
  "blurb": "test",
  "wallpaper": "` + wallpaper + `",
  "colors": {"accent": "#111111"},
  "term": {},
  "gtk": {"theme": "T", "icons": "I", "kvantum": "K", "gtk4": ""},
  "editors": {"doom": "d", "nvim": "n"}
}`
}
