package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Key order is the whole reason Ordered exists: Colors.qml and theme.lua emit
// one line per colour, so a plain Go map would reshuffle both files on every
// single run and turn every theme switch into a spurious diff.
func TestOrderedPreservesKeyOrder(t *testing.T) {
	var o Ordered
	// Deliberately not alphabetical, and not the order a Go map would produce.
	in := `{"zebra":"#1","alpha":"#2","middle":"#3","beta":"#4"}`
	if err := json.Unmarshal([]byte(in), &o); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	want := []string{"zebra", "alpha", "middle", "beta"}
	if !slices.Equal(o.Keys(), want) {
		t.Errorf("Keys() = %v, want %v", o.Keys(), want)
	}
	for i, k := range want {
		if got, expect := o.Get(k), "#"+string(rune('1'+i)); got != expect {
			t.Errorf("Get(%q) = %q, want %q", k, got, expect)
		}
	}
}

// Unmarshalling the same bytes repeatedly must give the same order, or the
// generated files would churn between runs.
func TestOrderedIsStableAcrossRuns(t *testing.T) {
	in := []byte(`{"one":"#1","two":"#2","three":"#3","four":"#4","five":"#5","six":"#6"}`)
	var first Ordered
	if err := json.Unmarshal(in, &first); err != nil {
		t.Fatal(err)
	}
	for range 50 {
		var o Ordered
		if err := json.Unmarshal(in, &o); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(o.Keys(), first.Keys()) {
			t.Fatalf("key order varied between runs: %v then %v", first.Keys(), o.Keys())
		}
	}
}

func TestOrderedGetUnknownKey(t *testing.T) {
	var o Ordered
	if err := json.Unmarshal([]byte(`{"a":"#1"}`), &o); err != nil {
		t.Fatal(err)
	}
	if got := o.Get("nope"); got != "" {
		t.Errorf("Get(missing) = %q, want \"\"", got)
	}
}

func TestOrderedDuplicateKeysListedOnce(t *testing.T) {
	var o Ordered
	if err := json.Unmarshal([]byte(`{"a":"#1","b":"#2","a":"#3"}`), &o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(o.Keys(), []string{"a", "b"}) {
		t.Errorf("Keys() = %v, want [a b] — a repeated key must not be listed twice", o.Keys())
	}
	// Last value wins, as it does in any JSON decoder.
	if got := o.Get("a"); got != "#3" {
		t.Errorf("Get(a) = %q, want %q", got, "#3")
	}
}

func TestOrderedEmptyObject(t *testing.T) {
	var o Ordered
	if err := json.Unmarshal([]byte(`{}`), &o); err != nil {
		t.Fatalf("Unmarshal of an empty object: %v", err)
	}
	if len(o.Keys()) != 0 {
		t.Errorf("Keys() = %v, want empty", o.Keys())
	}
}

func TestOrderedRejectsNonObjects(t *testing.T) {
	cases := []struct{ name, in string }{
		{"array", `["a","b"]`},
		{"string", `"nope"`},
		{"number", `12`},
		{"null", `null`},
		{"non-string value", `{"a":12}`},
		{"nested object value", `{"a":{"b":"c"}}`},
		{"truncated", `{"a":`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var o Ordered
			if err := json.Unmarshal([]byte(c.in), &o); err == nil {
				t.Errorf("Unmarshal(%s) returned nil error, want a failure", c.in)
			}
		})
	}
}

// A second Unmarshal into the same value must not accumulate the first one's
// keys.
func TestOrderedResetsBetweenUnmarshals(t *testing.T) {
	var o Ordered
	if err := json.Unmarshal([]byte(`{"a":"#1","b":"#2"}`), &o); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"c":"#3"}`), &o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(o.Keys(), []string{"c"}) {
		t.Errorf("Keys() = %v, want [c]", o.Keys())
	}
	if got := o.Get("a"); got != "" {
		t.Errorf("Get(a) = %q after re-unmarshalling, want \"\"", got)
	}
}

// -- Load / Names / Current --------------------------------------------------

func TestLoad(t *testing.T) {
	fixtureHome(t)

	th := loadFixture(t)
	if th.Name != "fixture" || th.Label != "Fixture" || th.Blurb != "a palette for tests" {
		t.Errorf("metadata = (%q, %q, %q)", th.Name, th.Label, th.Blurb)
	}
	if th.GTK.Theme != "Material-Black-Fixture" || th.GTK.Icons != "MB-Fixture-Suru-GLOW" ||
		th.GTK.Kvantum != "Fixture" || th.GTK.GTK4 != "Colloid-Teal-Dark-Nord" {
		t.Errorf("gtk block = %+v", th.GTK)
	}
	if th.Editors.Doom != "doom-fixture" || th.Editors.Nvim != "fixturefox" {
		t.Errorf("editors = %+v", th.Editors)
	}
	if got := th.Color("accent"); got != "#111111" {
		t.Errorf("Color(accent) = %q, want #111111", got)
	}
	if got := th.term("bg"); got != "#201201" {
		t.Errorf("term(bg) = %q, want #201201", got)
	}
	if !strings.Contains(th.banner, "themes/fixture.json") {
		t.Errorf("banner = %q, want it to name the source palette", th.banner)
	}
}

func TestLoadPreservesPaletteOrder(t *testing.T) {
	fixtureHome(t)
	th := loadFixture(t)

	want := []string{
		"base", "dim", "surface", "surfaceBright", "outline", "fg", "fgDim",
		"fgFaint", "comment", "red", "green", "yellow", "blue", "purple",
		"cyan", "orange", "accent",
	}
	if !slices.Equal(th.Colors.Keys(), want) {
		t.Errorf("colour order = %v,\nwant %v", th.Colors.Keys(), want)
	}
}

func TestLoadUnknownTheme(t *testing.T) {
	newHome(t, map[string]string{"one": fixturePalette, "two": fixturePalette})

	_, err := Load("nope")
	if err == nil {
		t.Fatal("Load of a missing theme returned nil error")
	}
	// The error is what a mistyped `theme set` prints, so it must list the
	// alternatives.
	for _, want := range []string{"nope", "one", "two"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	home := newHome(t, nil)
	writeFile(t, filepath.Join(home, ".config/theme/themes/broken.json"), `{"name": `)

	if _, err := Load("broken"); err == nil {
		t.Error("Load of malformed JSON returned nil error")
	}
}

func TestNames(t *testing.T) {
	home := newHome(t, map[string]string{
		"zebra": fixturePalette, "alpha": fixturePalette, "middle": fixturePalette,
	})
	// Non-palette files in the directory must be ignored.
	writeFile(t, filepath.Join(home, ".config/theme/themes/README.md"), "notes")
	mkdir(t, filepath.Join(home, ".config/theme/themes/subdir"))

	got, err := Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	want := []string{"alpha", "middle", "zebra"}
	if !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v (sorted, .json only)", got, want)
	}
}

func TestNamesMissingDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := Names(); err == nil {
		t.Error("Names with no themes directory returned nil error")
	}
}

func TestCurrent(t *testing.T) {
	t.Run("reads the state file", func(t *testing.T) {
		home := newHome(t, map[string]string{"alpha": fixturePalette, "zebra": fixturePalette})
		writeFile(t, filepath.Join(home, ".config/theme/current"), "zebra\n")

		if got := Current(); got != "zebra" {
			t.Errorf("Current() = %q, want %q", got, "zebra")
		}
	})

	t.Run("falls back to the first palette when there is no state file", func(t *testing.T) {
		newHome(t, map[string]string{"alpha": fixturePalette, "zebra": fixturePalette})
		if got := Current(); got != "alpha" {
			t.Errorf("Current() = %q, want %q", got, "alpha")
		}
	})

	// A palette that was renamed or deleted must not strand the switcher.
	t.Run("falls back when the state names a palette that is gone", func(t *testing.T) {
		home := newHome(t, map[string]string{"alpha": fixturePalette})
		writeFile(t, filepath.Join(home, ".config/theme/current"), "deleted-theme\n")

		if got := Current(); got != "alpha" {
			t.Errorf("Current() = %q, want %q", got, "alpha")
		}
	})

	t.Run("empty when there are no palettes at all", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if got := Current(); got != "" {
			t.Errorf("Current() = %q, want \"\"", got)
		}
	})

	t.Run("tolerates surrounding whitespace", func(t *testing.T) {
		home := newHome(t, map[string]string{"alpha": fixturePalette, "zebra": fixturePalette})
		writeFile(t, filepath.Join(home, ".config/theme/current"), "  zebra \n\n")
		if got := Current(); got != "zebra" {
			t.Errorf("Current() = %q, want %q", got, "zebra")
		}
	})
}

func TestWallpaperPath(t *testing.T) {
	home := fixtureHome(t)
	th := loadFixture(t)

	want := filepath.Join(home, ".config/hypr/wallpapers", "test.jpg")
	if got := th.wallpaperPath(); got != want {
		t.Errorf("wallpaperPath() = %q, want %q", got, want)
	}
}

// The real palettes must parse and carry every key the renderers read; a typo
// in one of them would otherwise only surface as an empty colour in a config.
func TestRealPalettesAreComplete(t *testing.T) {
	real := filepath.Join(os.Getenv("HOME"), ".config/theme/themes")
	entries, err := os.ReadDir(real)
	if err != nil {
		t.Skipf("no palettes installed at %s", real)
	}

	home := newHome(t, nil)
	found := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(real, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(home, ".config/theme/themes", e.Name()), string(raw))
		found++
	}
	if found == 0 {
		t.Skip("no palettes to check")
	}

	names, err := Names()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			th, err := Load(n)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			for _, k := range requiredColors {
				if th.Color(k) == "" {
					t.Errorf("colour %q is missing", k)
				}
			}
			for _, k := range requiredTerm {
				if th.Term.Get(k) == "" {
					t.Errorf("term colour %q is missing", k)
				}
			}
			if th.Name == "" || th.Label == "" || th.Wallpaper == "" {
				t.Errorf("name/label/wallpaper incomplete: %q/%q/%q", th.Name, th.Label, th.Wallpaper)
			}
			if th.GTK.Theme == "" || th.GTK.Icons == "" || th.GTK.Kvantum == "" {
				t.Errorf("gtk block incomplete: %+v", th.GTK)
			}
			if th.Editors.Doom == "" || th.Editors.Nvim == "" {
				t.Errorf("editors incomplete: %+v", th.Editors)
			}
		})
	}
}

// Every key the renderers actually read.
var (
	requiredColors = []string{
		"base", "dim", "surface", "surfaceBright", "outline", "fg", "fgDim",
		"fgFaint", "comment", "red", "green", "yellow", "blue", "purple",
		"cyan", "orange", "accent",
	}
	requiredTerm = []string{
		"bg", "fg", "cursor", "selectionBg", "black", "brightBlack", "white", "brightWhite",
	}
)
