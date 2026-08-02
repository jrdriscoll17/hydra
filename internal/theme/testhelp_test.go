package theme

import (
	"os"
	"path/filepath"
	"testing"
)

// fixturePalette mirrors the real schema, with values chosen so every renderer
// slot is distinguishable in the output — no two colours repeat, so a renderer
// wiring the wrong key into the wrong slot shows up rather than coincidentally
// matching.
const fixturePalette = `{
  "name": "fixture",
  "label": "Fixture",
  "blurb": "a palette for tests",
  "wallpaper": "test.jpg",
  "colors": {
    "base": "#010101",
    "dim": "#020202",
    "surface": "#030303",
    "surfaceBright": "#040404",
    "outline": "#050505",
    "fg": "#060606",
    "fgDim": "#070707",
    "fgFaint": "#080808",
    "comment": "#090909",
    "red": "#0a0a0a",
    "green": "#0b0b0b",
    "yellow": "#0c0c0c",
    "blue": "#0d0d0d",
    "purple": "#0e0e0e",
    "cyan": "#0f0f0f",
    "orange": "#101010",
    "accent": "#111111"
  },
  "term": {
    "bg": "#201201",
    "fg": "#202202",
    "cursor": "#203203",
    "selectionBg": "#204204",
    "black": "#205205",
    "brightBlack": "#206206",
    "white": "#207207",
    "brightWhite": "#208208"
  },
  "gtk": {
    "theme": "Material-Black-Fixture",
    "icons": "MB-Fixture-Suru-GLOW",
    "kvantum": "Fixture",
    "gtk4": "Colloid-Teal-Dark-Nord"
  },
  "editors": {
    "doom": "doom-fixture",
    "nvim": "fixturefox"
  }
}`

// newHome points HOME at a temp dir and installs the given palettes there.
// Every path the package touches derives from HOME, so this isolates a test
// completely from the real machine.
func newHome(t *testing.T, palettes map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	for name, body := range palettes {
		writeFile(t, filepath.Join(home, ".config/theme/themes", name+".json"), body)
	}
	return home
}

// fixtureHome is the common case: one palette named "fixture".
func fixtureHome(t *testing.T) string {
	t.Helper()
	return newHome(t, map[string]string{"fixture": fixturePalette})
}

// loadFixture returns the parsed fixture palette.
func loadFixture(t *testing.T) *Theme {
	t.Helper()
	th, err := Load("fixture")
	if err != nil {
		t.Fatalf("loading the fixture palette: %v", err)
	}
	return th
}

// noReload stubs out the live-reload side effects for the duration of a test.
// reloadAll drives gsettings and hyprctl, which would reach the real desktop.
func noReload(t *testing.T) {
	t.Helper()
	original := reload
	reload = func(*Theme) {}
	t.Cleanup(func() { reload = original })
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
