package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sizeRank is the one deliberate behaviour change from theme.py, so it gets
// tested on its own terms rather than against the Python output.
func TestSizeRank(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		scalable, px int
	}{
		{"scalable outranks every fixed size", "scalable", 1, 0},
		{"scalable with a suffix", "scalable-up-to-32", 1, 0},
		{"plain size", "32x32", 0, 32},
		{"large size", "512x512", 0, 512},
		// "32x32@2x" holds 64px artwork, so it must outrank a plain 32x32.
		// theme.py stripped the suffix, tying the two and letting readdir order
		// pick the winner — which is why its icons.json was not reproducible.
		{"@2x counts double", "32x32@2x", 0, 64},
		{"@3x counts triple", "16x16@3x", 0, 48},
		{"@2x on a large size", "256x256@2x", 0, 512},
		{"non-numeric scale is ignored", "32x32@xx", 0, 32},
		{"unparseable", "symbolic", 0, 0},
		{"empty", "", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotS, gotPx := sizeRank(c.in)
			if gotS != c.scalable || gotPx != c.px {
				t.Errorf("sizeRank(%q) = (%d, %d), want (%d, %d)", c.in, gotS, gotPx, c.scalable, c.px)
			}
		})
	}
}

func TestSizeRankOrdersAtTwoXAboveItsBase(t *testing.T) {
	_, plain := sizeRank("32x32")
	_, retina := sizeRank("32x32@2x")
	if retina <= plain {
		t.Errorf("32x32@2x ranks %d, not above 32x32's %d — the tie this was "+
			"meant to break is back, and icons.json stops being reproducible", retina, plain)
	}
}

func TestSortBySizeDesc(t *testing.T) {
	got := []string{"16x16", "512x512", "scalable", "32x32", "32x32@2x", "48x48"}
	sortBySizeDesc(got)

	// 32x32@2x sorts above 48x48 because it holds 64px of artwork — ranking by
	// effective pixels rather than by the nominal size is the whole point.
	want := []string{"scalable", "512x512", "32x32@2x", "48x48", "32x32", "16x16"}
	if !slices.Equal(got, want) {
		t.Errorf("sortBySizeDesc = %v, want %v", got, want)
	}
}

// Equal ranks must keep their input order, matching Python's stable sort.
func TestSortBySizeDescIsStable(t *testing.T) {
	in := []string{"weird", "symbolic", "unparseable", "32x32"}
	got := slices.Clone(in)
	sortBySizeDesc(got)

	want := []string{"32x32", "weird", "symbolic", "unparseable"}
	if !slices.Equal(got, want) {
		t.Errorf("sortBySizeDesc = %v, want %v (equal ranks keep input order)", got, want)
	}
}

func TestCompactSortedJSON(t *testing.T) {
	got, err := compactSortedJSON(map[string]string{
		"zebra": "/z.svg", "alpha": "/a.svg", "middle": "/m.svg",
	})
	if err != nil {
		t.Fatalf("compactSortedJSON: %v", err)
	}
	want := `{"alpha":"/a.svg","middle":"/m.svg","zebra":"/z.svg"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("output ends in a newline; json.dumps does not")
	}
}

// Icon names really do contain & and <, and escaping them would break the
// lookup in Icons.qml.
func TestCompactSortedJSONDoesNotEscapeHTML(t *testing.T) {
	got, err := compactSortedJSON(map[string]string{"a&b<c>d": "/x.svg"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"a&b<c>d"`) {
		t.Errorf("got %q, want the key unescaped", got)
	}
}

func TestCompactSortedJSONEmpty(t *testing.T) {
	got, err := compactSortedJSON(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "{}" {
		t.Errorf("got %q, want %q", got, "{}")
	}
}

// -- iconThemeChain ----------------------------------------------------------

// iconTree builds a theme directory, optionally with an Inherits line.
func iconTree(t *testing.T, root, theme, inherits string, dirs ...string) {
	t.Helper()
	index := "[Icon Theme]\nName=" + theme + "\n"
	if inherits != "" {
		index += "Inherits=" + inherits + "\n"
	}
	writeFile(t, filepath.Join(root, theme, "index.theme"), index)
	for _, d := range dirs {
		mkdir(t, filepath.Join(root, theme, d))
	}
}

// useIconRoots points the lookup at a fixture instead of the real system.
func useIconRoots(t *testing.T, roots ...string) {
	t.Helper()
	original := iconRoots
	iconRoots = func() []string { return roots }
	t.Cleanup(func() { iconRoots = original })
}

func TestIconThemeChain(t *testing.T) {
	root := t.TempDir()
	iconTree(t, root, "MB-Fixture-Suru-GLOW", "Papirus-Dark,Papirus", "apps")
	iconTree(t, root, "Papirus-Dark", "Papirus", "apps")
	iconTree(t, root, "Papirus", "hicolor", "apps")
	iconTree(t, root, "hicolor", "", "apps")
	useIconRoots(t, root)

	got := iconThemeChain("MB-Fixture-Suru-GLOW")
	want := []string{
		filepath.Join(root, "MB-Fixture-Suru-GLOW"),
		filepath.Join(root, "hicolor"),
		filepath.Join(root, "Papirus-Dark"),
		filepath.Join(root, "Papirus"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("chain = %v,\nwant %v", got, want)
	}
}

// Themes inherit each other in loops in the wild; the walk must terminate.
func TestIconThemeChainHandlesCycles(t *testing.T) {
	root := t.TempDir()
	iconTree(t, root, "A", "B", "apps")
	iconTree(t, root, "B", "A", "apps")
	iconTree(t, root, "hicolor", "", "apps")
	useIconRoots(t, root)

	got := iconThemeChain("A")
	if len(got) != 3 {
		t.Errorf("chain = %v, want three entries with no repeats", got)
	}
	seen := map[string]bool{}
	for _, d := range got {
		if seen[d] {
			t.Errorf("chain visits %s twice", d)
		}
		seen[d] = true
	}
}

func TestIconThemeChainAlwaysIncludesHicolor(t *testing.T) {
	root := t.TempDir()
	iconTree(t, root, "Standalone", "", "apps")
	iconTree(t, root, "hicolor", "", "apps")
	useIconRoots(t, root)

	got := iconThemeChain("Standalone")
	if !slices.Contains(got, filepath.Join(root, "hicolor")) {
		t.Errorf("chain = %v, want hicolor as the final fallback", got)
	}
}

func TestIconThemeChainSkipsAbsentThemes(t *testing.T) {
	root := t.TempDir()
	iconTree(t, root, "Present", "NotInstalled", "apps")
	useIconRoots(t, root)

	got := iconThemeChain("Present")
	if !slices.Equal(got, []string{filepath.Join(root, "Present")}) {
		t.Errorf("chain = %v, want just the installed theme", got)
	}
}

// Earlier roots take precedence, but a theme present in several roots
// contributes all of them, so a user override can supplement a system theme.
func TestIconThemeChainSpansRootsInOrder(t *testing.T) {
	user, system := t.TempDir(), t.TempDir()
	iconTree(t, user, "Shared", "", "apps")
	iconTree(t, system, "Shared", "", "apps")
	useIconRoots(t, user, system)

	got := iconThemeChain("Shared")
	want := []string{filepath.Join(user, "Shared"), filepath.Join(system, "Shared")}
	if !slices.Equal(got, want) {
		t.Errorf("chain = %v, want %v", got, want)
	}
}

// -- renderIcons -------------------------------------------------------------

// iconFixture lays out both directory conventions renderIcons has to cope with
// and returns the icon root.
func iconFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Suru layout: category/size.
	writeFile(t, filepath.Join(root, "MB-Fixture-Suru-GLOW", "index.theme"),
		"[Icon Theme]\nName=MB-Fixture-Suru-GLOW\nInherits=hicolor\n")
	writeFile(t, filepath.Join(root, "MB-Fixture-Suru-GLOW", "apps", "48x48", "firefox.svg"), "<svg/>")
	writeFile(t, filepath.Join(root, "MB-Fixture-Suru-GLOW", "apps", "16x16", "firefox.svg"), "<svg small/>")
	writeFile(t, filepath.Join(root, "MB-Fixture-Suru-GLOW", "status", "24x24", "battery.png"), "png")
	// Not an indexed category, so it must be skipped.
	writeFile(t, filepath.Join(root, "MB-Fixture-Suru-GLOW", "mimetypes", "48x48", "text.svg"), "<svg/>")
	// Not an icon extension.
	writeFile(t, filepath.Join(root, "MB-Fixture-Suru-GLOW", "apps", "48x48", "notes.txt"), "x")

	// Papirus layout: size/category.
	writeFile(t, filepath.Join(root, "hicolor", "index.theme"), "[Icon Theme]\nName=hicolor\n")
	writeFile(t, filepath.Join(root, "hicolor", "32x32", "apps", "fallback.png"), "png")
	writeFile(t, filepath.Join(root, "hicolor", "scalable", "apps", "vector.svg"), "<svg/>")

	useIconRoots(t, root)
	return root
}

func loadIndex(t *testing.T, path string) map[string]string {
	t.Helper()
	var index map[string]string
	if err := json.Unmarshal([]byte(readFile(t, path)), &index); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return index
}

func TestRenderIcons(t *testing.T) {
	home := fixtureHome(t)
	root := iconFixture(t)
	th := loadFixture(t)

	if err := renderIcons(th); err != nil {
		t.Fatalf("renderIcons: %v", err)
	}

	out := filepath.Join(home, ".config/quickshell/generated/icons.json")
	index := loadIndex(t, out)

	t.Run("bigger artwork wins within a theme", func(t *testing.T) {
		want := filepath.Join(root, "MB-Fixture-Suru-GLOW", "apps", "48x48", "firefox.svg")
		if index["firefox"] != want {
			t.Errorf("firefox -> %q, want the 48x48 source %q", index["firefox"], want)
		}
	})

	t.Run("both directory layouts are walked", func(t *testing.T) {
		for _, name := range []string{"firefox", "battery", "fallback", "vector"} {
			if index[name] == "" {
				t.Errorf("%q is missing from the index", name)
			}
		}
	})

	t.Run("uninteresting categories and extensions are skipped", func(t *testing.T) {
		if _, ok := index["text"]; ok {
			t.Error("an icon from the mimetypes category was indexed")
		}
		if _, ok := index["notes"]; ok {
			t.Error("a .txt file was indexed")
		}
	})

	t.Run("the theme marker is written", func(t *testing.T) {
		marker := readFile(t, filepath.Join(home, ".config/quickshell/generated/icons.theme"))
		if strings.TrimSpace(marker) != "MB-Fixture-Suru-GLOW" {
			t.Errorf("marker = %q, want the icon theme name", marker)
		}
	})

	t.Run("the cache is populated", func(t *testing.T) {
		cache := filepath.Join(home, ".cache/theme", "icons-MB-Fixture-Suru-GLOW.json")
		if _, err := os.Stat(cache); err != nil {
			t.Errorf("no cache file: %v", err)
		}
	})
}

// The earlier theme in the chain wins, which is what makes the palette's own
// icon set override hicolor.
func TestRenderIconsPrefersTheThemeOverItsFallback(t *testing.T) {
	home := fixtureHome(t)
	root := iconFixture(t)
	// Same icon name in both themes, at a *larger* size in the fallback.
	writeFile(t, filepath.Join(root, "hicolor", "512x512", "apps", "firefox.png"), "png")
	th := loadFixture(t)

	if err := renderIcons(th); err != nil {
		t.Fatalf("renderIcons: %v", err)
	}
	index := loadIndex(t, filepath.Join(home, ".config/quickshell/generated/icons.json"))

	if !strings.Contains(index["firefox"], "MB-Fixture-Suru-GLOW") {
		t.Errorf("firefox -> %q, want the theme's own icon to beat hicolor's larger one",
			index["firefox"])
	}
}

// Rebuilding costs ~1.6s of disk walking, so an unchanged theme must short-
// circuit on the marker.
func TestRenderIconsSkipsWhenTheMarkerMatches(t *testing.T) {
	home := fixtureHome(t)
	iconFixture(t)
	th := loadFixture(t)

	out := filepath.Join(home, ".config/quickshell/generated/icons.json")
	writeFile(t, out, `{"stale":"/stale.svg"}`)
	writeFile(t, filepath.Join(home, ".config/quickshell/generated/icons.theme"),
		"MB-Fixture-Suru-GLOW\n")

	if err := renderIcons(th); err != nil {
		t.Fatalf("renderIcons: %v", err)
	}
	if got := readFile(t, out); got != `{"stale":"/stale.svg"}` {
		t.Error("the index was rebuilt even though the marker already named this theme")
	}
}

// Switching themes must reuse a previously cached index rather than walking
// the disk again.
func TestRenderIconsRestoresFromCache(t *testing.T) {
	home := fixtureHome(t)
	iconFixture(t)
	th := loadFixture(t)

	cached := `{"from":"/cache.svg"}`
	writeFile(t, filepath.Join(home, ".cache/theme", "icons-MB-Fixture-Suru-GLOW.json"), cached)

	if err := renderIcons(th); err != nil {
		t.Fatalf("renderIcons: %v", err)
	}
	out := filepath.Join(home, ".config/quickshell/generated/icons.json")
	if got := readFile(t, out); got != cached {
		t.Errorf("index = %q, want the cached copy %q", got, cached)
	}
	marker := readFile(t, filepath.Join(home, ".config/quickshell/generated/icons.theme"))
	if strings.TrimSpace(marker) != "MB-Fixture-Suru-GLOW" {
		t.Errorf("marker = %q", marker)
	}
}

// A different theme has a different cache key, so its index must be rebuilt.
func TestRenderIconsRebuildsWhenTheThemeChanges(t *testing.T) {
	home := fixtureHome(t)
	iconFixture(t)
	th := loadFixture(t)

	writeFile(t, filepath.Join(home, ".config/quickshell/generated/icons.json"), `{"stale":"/x.svg"}`)
	writeFile(t, filepath.Join(home, ".config/quickshell/generated/icons.theme"), "Some-Other-Theme\n")

	if err := renderIcons(th); err != nil {
		t.Fatalf("renderIcons: %v", err)
	}
	index := loadIndex(t, filepath.Join(home, ".config/quickshell/generated/icons.json"))
	if _, stale := index["stale"]; stale {
		t.Error("the index was not rebuilt after the icon theme changed")
	}
	if index["firefox"] == "" {
		t.Error("the rebuilt index is missing icons")
	}
}

func TestRenderIconsIsDeterministic(t *testing.T) {
	home := fixtureHome(t)
	iconFixture(t)
	th := loadFixture(t)
	out := filepath.Join(home, ".config/quickshell/generated/icons.json")

	if err := renderIcons(th); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, out)

	// Clear the short-circuits so it genuinely walks again.
	for range 3 {
		if err := os.Remove(filepath.Join(home, ".config/quickshell/generated/icons.theme")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(home, ".cache/theme", "icons-MB-Fixture-Suru-GLOW.json")); err != nil {
			t.Fatal(err)
		}
		if err := renderIcons(th); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, out); got != first {
			t.Fatalf("icons.json changed between rebuilds:\n%s\nvs\n%s", first, got)
		}
	}
}

func TestRenderIconsWithNoIconsInstalled(t *testing.T) {
	home := fixtureHome(t)
	useIconRoots(t, filepath.Join(t.TempDir(), "empty"))
	th := loadFixture(t)

	if err := renderIcons(th); err != nil {
		t.Fatalf("renderIcons with no icon themes returned %v, want nil", err)
	}
	// Still writes an index, so Icons.qml has something valid to read.
	if got := readFile(t, filepath.Join(home, ".config/quickshell/generated/icons.json")); got == "" {
		t.Error("no index written")
	}
}
