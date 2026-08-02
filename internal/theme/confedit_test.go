package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripHash(t *testing.T) {
	cases := map[string]string{
		"#aabbcc": "aabbcc",
		"aabbcc":  "aabbcc",
		"":        "",
		"#":       "",
	}
	for in, want := range cases {
		if got := stripHash(in); got != want {
			t.Errorf("stripHash(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRGBAColor(t *testing.T) {
	cases := []struct {
		c    string
		a    float64
		want string
	}{
		{"#aabbcc", 1, "rgba(aabbccff)"},
		{"#aabbcc", 0, "rgba(aabbcc00)"},
		// The two alphas the window-border reload actually uses.
		{"#111111", 0.93, "rgba(111111ed)"},
		{"#010101", 0.67, "rgba(010101ab)"},
		{"#ffffff", 0.5, "rgba(ffffff80)"},
	}
	for _, c := range cases {
		if got := rgbaColor(c.c, c.a); got != c.want {
			t.Errorf("rgbaColor(%q, %v) = %q, want %q", c.c, c.a, got, c.want)
		}
	}
}

func TestRoundHalfUp(t *testing.T) {
	cases := map[float64]float64{
		0.4: 0, 0.5: 1, 0.6: 1, 1.5: 2, 2.5: 3,
		236.14999999999998: 236, // 0.93 * 255
		170.85:             171, // 0.67 * 255
		255:                255,
	}
	for in, want := range cases {
		if got := roundHalfUp(in); got != want {
			t.Errorf("roundHalfUp(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestSubLine(t *testing.T) {
	t.Run("replaces every matching line", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf")
		writeFile(t, path, "a=1\ncolor_theme = \"old\"\nb=2\n")

		if err := subLine(path, `^color_theme = ".*"$`, `color_theme = "new"`); err != nil {
			t.Fatalf("subLine: %v", err)
		}
		want := "a=1\ncolor_theme = \"new\"\nb=2\n"
		if got := readFile(t, path); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// This is how a config ends up stranded on the previous theme, and the
	// reason setINIKey exists for the files that matter.
	t.Run("does nothing when the key is absent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf")
		writeFile(t, path, "a=1\n")

		if err := subLine(path, `^color_theme = ".*"$`, `color_theme = "new"`); err != nil {
			t.Fatalf("subLine: %v", err)
		}
		if got := readFile(t, path); got != "a=1\n" {
			t.Errorf("got %q, want it unchanged", got)
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		if err := subLine(filepath.Join(t.TempDir(), "nope"), `^x$`, "y"); err != nil {
			t.Errorf("subLine on a missing file returned %v, want nil", err)
		}
	})

	t.Run("invalid pattern", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf")
		writeFile(t, path, "a=1\n")
		if err := subLine(path, `[unclosed`, "y"); err == nil {
			t.Error("subLine with an invalid pattern returned nil error")
		}
	})

	t.Run("does not rewrite when nothing changed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf")
		writeFile(t, path, "color_theme = \"same\"\n")
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := subLine(path, `^color_theme = ".*"$`, `color_theme = "same"`); err != nil {
			t.Fatal(err)
		}
		after, _ := os.Stat(path)
		if !before.ModTime().Equal(after.ModTime()) {
			t.Error("file rewritten despite identical content")
		}
	})
}

func TestSetINIKey(t *testing.T) {
	t.Run("creates the file, the section and the key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sub", "settings.ini")

		if err := setINIKey(path, "Settings", "gtk-theme-name", "X"); err != nil {
			t.Fatalf("setINIKey: %v", err)
		}
		want := "[Settings]\ngtk-theme-name=X\n"
		if got := readFile(t, path); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("replaces an existing key in place", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.ini")
		writeFile(t, path, "[Settings]\ngtk-theme-name=Old\ngtk-font-name=Ubuntu 11\n")

		if err := setINIKey(path, "Settings", "gtk-theme-name", "New"); err != nil {
			t.Fatalf("setINIKey: %v", err)
		}
		want := "[Settings]\ngtk-theme-name=New\ngtk-font-name=Ubuntu 11\n"
		if got := readFile(t, path); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("inserts under an existing section", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.ini")
		writeFile(t, path, "[Settings]\ngtk-font-name=Ubuntu 11\n")

		if err := setINIKey(path, "Settings", "gtk-theme-name", "New"); err != nil {
			t.Fatalf("setINIKey: %v", err)
		}
		want := "[Settings]\ngtk-theme-name=New\ngtk-font-name=Ubuntu 11\n"
		if got := readFile(t, path); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("appends a missing section to an existing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "qt6ct.conf")
		writeFile(t, path, "[Fonts]\nfixed=x\n")

		if err := setINIKey(path, "Appearance", "style", "kvantum"); err != nil {
			t.Fatalf("setINIKey: %v", err)
		}
		want := "[Fonts]\nfixed=x\n\n[Appearance]\nstyle=kvantum\n"
		if got := readFile(t, path); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// A key found anywhere wins, even under a different section — the configs
	// this touches only ever declare these keys once.
	t.Run("an existing key outside the named section is still replaced", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf")
		writeFile(t, path, "[Other]\nstyle=Fusion\n")

		if err := setINIKey(path, "Appearance", "style", "kvantum"); err != nil {
			t.Fatalf("setINIKey: %v", err)
		}
		if got := readFile(t, path); got != "[Other]\nstyle=kvantum\n" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("repeated calls are idempotent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.ini")
		for range 3 {
			if err := setINIKey(path, "Settings", "k", "v"); err != nil {
				t.Fatal(err)
			}
		}
		got := readFile(t, path)
		if strings.Count(got, "k=v") != 1 || strings.Count(got, "[Settings]") != 1 {
			t.Errorf("not idempotent:\n%s", got)
		}
	})

	t.Run("values containing = survive", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf")
		if err := setINIKey(path, "S", "k", "a=b=c"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, path); !strings.Contains(got, "k=a=b=c") {
			t.Errorf("got %q", got)
		}
	})
}

func TestThemeSearch(t *testing.T) {
	t.Run("finds a theme in ~/.themes", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".themes", "Colloid-Dark")
		mkdir(t, dir)

		if got := themeSearch("Colloid-Dark"); got != dir {
			t.Errorf("themeSearch = %q, want %q", got, dir)
		}
	})

	t.Run("finds a theme in ~/.local/share/themes", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".local/share/themes", "Colloid-Dark")
		mkdir(t, dir)

		if got := themeSearch("Colloid-Dark"); got != dir {
			t.Errorf("themeSearch = %q, want %q", got, dir)
		}
	})

	t.Run("~/.themes wins over the shared location", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		first := filepath.Join(home, ".themes", "X")
		mkdir(t, first)
		mkdir(t, filepath.Join(home, ".local/share/themes", "X"))

		if got := themeSearch("X"); got != first {
			t.Errorf("themeSearch = %q, want %q", got, first)
		}
	})

	t.Run("empty when absent", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if got := themeSearch("Nope"); got != "" {
			t.Errorf("themeSearch = %q, want \"\"", got)
		}
	})

	t.Run("a file of the right name does not count", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeFile(t, filepath.Join(home, ".themes", "X"), "not a directory")

		if got := themeSearch("X"); got != "" {
			t.Errorf("themeSearch = %q, want \"\" for a non-directory", got)
		}
	})
}
