package setup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSettingKey(t *testing.T) {
	cases := map[string]string{
		"gtk-theme-name=Material-Black-IceBlue":                  "gtk-theme-name",
		`color_theme = "/home/jake/.config/btop/themes/x.theme"`: "color_theme",
		"icon_theme=MB-IceBlue-Suru-GLOW":                        "icon_theme",
		"  spaced  =  value":                                     "spaced",
		"[Settings]":                                             "",
		"":                                                       "",
		"# a comment":                                            "",
	}
	for in, want := range cases {
		if got := settingKey(in); got != want {
			t.Errorf("settingKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinesDiffer(t *testing.T) {
	t.Run("identical", func(t *testing.T) {
		if got := linesDiffer("a\nb\nc", "a\nb\nc"); len(got) != 0 {
			t.Errorf("linesDiffer = %v, want none", got)
		}
	})

	t.Run("one line changed", func(t *testing.T) {
		got := linesDiffer("a\nb\nc", "a\nB\nc")
		slices.Sort(got)
		if !slices.Equal(got, []string{"B", "b"}) {
			t.Errorf("linesDiffer = %v, want [B b]", got)
		}
	})

	t.Run("line added", func(t *testing.T) {
		got := linesDiffer("a\nb", "a\nb\nc")
		if !slices.Equal(got, []string{"c"}) {
			t.Errorf("linesDiffer = %v, want [c]", got)
		}
	})

	// Reordering alone is not a content difference worth reporting.
	t.Run("reordered", func(t *testing.T) {
		if got := linesDiffer("a\nb", "b\na"); len(got) != 0 {
			t.Errorf("linesDiffer = %v, want none for a pure reorder", got)
		}
	})
}

// chezmoiStub makes `chezmoi cat <path>` return the given contents, so the
// comparison can be tested without a real chezmoi source.
func chezmoiStub(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n  cat) cat <<'CHEZMOI_EOF'\n" +
		contents + "\nCHEZMOI_EOF\n  ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "chezmoi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestThemeOwnedDrift(t *testing.T) {
	repo := strings.Join([]string{
		"[Settings]",
		"gtk-theme-name=Material-Black-Evergreen",
		"gtk-icon-theme-name=MB-Evergreen-Suru-GLOW",
		"gtk-font-name=Ubuntu Nerd Font 11",
		"gtk-application-prefer-dark-theme=1",
	}, "\n")

	t.Run("only the theme lines differ", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		chezmoiStub(t, repo)
		// This machine is on a different theme, which is the whole scenario.
		writeFile(t, filepath.Join(home, ".config/gtk-3.0/settings.ini"), strings.Join([]string{
			"[Settings]",
			"gtk-theme-name=Material-Black-IceBlue",
			"gtk-icon-theme-name=MB-IceBlue-Suru-GLOW",
			"gtk-font-name=Ubuntu Nerd Font 11",
			"gtk-application-prefer-dark-theme=1",
		}, "\n"))

		if !themeOwnedDrift(".config/gtk-3.0/settings.ini") {
			t.Error("themeOwnedDrift = false when only the switcher's own lines differ")
		}
	})

	// A real edit alongside the theme lines still needs a decision.
	t.Run("a real setting also differs", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		chezmoiStub(t, repo)
		writeFile(t, filepath.Join(home, ".config/gtk-3.0/settings.ini"), strings.Join([]string{
			"[Settings]",
			"gtk-theme-name=Material-Black-IceBlue",
			"gtk-icon-theme-name=MB-IceBlue-Suru-GLOW",
			"gtk-font-name=Comic Sans 40", // not the switcher's business
			"gtk-application-prefer-dark-theme=1",
		}, "\n"))

		if themeOwnedDrift(".config/gtk-3.0/settings.ini") {
			t.Error("themeOwnedDrift = true despite an unrelated setting differing")
		}
	})

	t.Run("identical files are not drift", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		chezmoiStub(t, repo)
		writeFile(t, filepath.Join(home, ".config/gtk-3.0/settings.ini"), repo)

		if themeOwnedDrift(".config/gtk-3.0/settings.ini") {
			t.Error("themeOwnedDrift = true for identical content")
		}
	})

	// Only the listed files are eligible; anything else is a real conflict.
	t.Run("an untracked path is never theme drift", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		chezmoiStub(t, "anything")
		writeFile(t, filepath.Join(home, ".config/nvim/init.lua"), "different")

		if themeOwnedDrift(".config/nvim/init.lua") {
			t.Error("themeOwnedDrift = true for a file the switcher does not touch")
		}
	})

	t.Run("btop's spaced assignment", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		chezmoiStub(t, "theme_background = False\ncolor_theme = \"/home/jake/.config/btop/themes/evergreen.theme\"")
		writeFile(t, filepath.Join(home, ".config/btop/btop.conf"),
			"theme_background = False\ncolor_theme = \"/home/jake/.config/btop/themes/ice-blue.theme\"")

		if !themeOwnedDrift(".config/btop/btop.conf") {
			t.Error("themeOwnedDrift = false for btop's `key = value` convention")
		}
	})
}

func TestSplitThemeDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	chezmoiStub(t, "[Settings]\ngtk-theme-name=Material-Black-Evergreen")

	writeFile(t, filepath.Join(home, ".config/gtk-3.0/settings.ini"),
		"[Settings]\ngtk-theme-name=Material-Black-IceBlue")
	writeFile(t, filepath.Join(home, ".config/nvim/init.lua"), "mine")

	real, themed := splitThemeDrift([]string{".config/gtk-3.0/settings.ini", ".config/nvim/init.lua"})

	if !slices.Equal(themed, []string{".config/gtk-3.0/settings.ini"}) {
		t.Errorf("themed = %v, want the settings.ini", themed)
	}
	if !slices.Equal(real, []string{".config/nvim/init.lua"}) {
		t.Errorf("real = %v, want the init.lua", real)
	}
}

// Every path listed as theme-owned must be one a component actually manages,
// or the map is describing files nothing deploys.
func TestThemeOwnedPathsAreOwnedByAComponent(t *testing.T) {
	owned := map[string]bool{}
	for _, c := range catalog() {
		for _, p := range c.Paths {
			owned[p] = true
		}
	}

	for path := range themeOwnedKeys {
		claimed := owned[path]
		if !claimed {
			// A file inside an owned directory counts too.
			for p := range owned {
				if strings.HasPrefix(path, p+"/") {
					claimed = true
					break
				}
			}
		}
		if !claimed {
			t.Errorf("%q is listed as theme-owned but no component manages it", path)
		}
	}
}
