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
