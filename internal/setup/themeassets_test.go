package setup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jrdriscoll17/hydra/internal/theme"
)

// palette writes a theme definition naming the given GTK assets.
func palette(t *testing.T, home, name, gtkTheme, icons, gtk4, accent string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".config/theme/themes", name+".json"), `{
  "name": "`+name+`",
  "label": "`+name+`",
  "blurb": "test",
  "wallpaper": "w.jpg",
  "colors": {"accent": "`+accent+`"},
  "term": {},
  "gtk": {"theme": "`+gtkTheme+`", "icons": "`+icons+`", "kvantum": "K", "gtk4": "`+gtk4+`"},
  "editors": {"doom": "d", "nvim": "n"}
}`)
}

// installPair puts a Material-Black + Suru-GLOW pair on disk, complete enough
// for installedBase to accept it.
func installPair(t *testing.T, home, variant string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".themes", "Material-Black-"+variant, "gtk-3.0", "gtk.css"),
		"a{color:#00e5ce}")
	writeFile(t, filepath.Join(home, ".local/share/icons", "MB-"+variant+"-Suru-GLOW",
		"places", "scalable", "folder.svg"), "<svg/>")
}

func TestVariantOf(t *testing.T) {
	cases := map[string]string{
		"Material-Black-IceBlue":   "IceBlue",
		"Material-Black-Evergreen": "Evergreen",
		"Colloid-Dark":             "Colloid-Dark", // no prefix to strip
		"":                         "",
	}
	for in, want := range cases {
		th := &theme.Theme{}
		th.GTK.Theme = in
		if got := variantOf(th); got != want {
			t.Errorf("variantOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssetsBuilt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	th := &theme.Theme{}
	th.GTK.Theme = "Material-Black-IceBlue"
	th.GTK.Icons = "MB-IceBlue-Suru-GLOW"

	if assetsBuilt(th) {
		t.Error("assetsBuilt = true on an empty HOME")
	}

	mkdir(t, filepath.Join(home, ".themes/Material-Black-IceBlue"))
	if assetsBuilt(th) {
		t.Error("assetsBuilt = true with only the GTK theme; the icon set is also needed")
	}

	mkdir(t, filepath.Join(home, ".local/share/icons/MB-IceBlue-Suru-GLOW"))
	if !assetsBuilt(th) {
		t.Error("assetsBuilt = false with both halves present")
	}
}

func TestInstalledBase(t *testing.T) {
	t.Run("nothing installed", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if got := installedBase(); got != "" {
			t.Errorf("installedBase = %q, want \"\"", got)
		}
	})

	t.Run("a complete pair is found", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		installPair(t, home, "IceBlue")

		if got := installedBase(); got != "IceBlue" {
			t.Errorf("installedBase = %q, want %q", got, "IceBlue")
		}
	})

	// A GTK theme with no matching icon set cannot seed a recolour, since
	// readBase needs both halves.
	t.Run("a half-installed pair is not a base", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeFile(t, filepath.Join(home, ".themes/Material-Black-Orphan/gtk-3.0/gtk.css"), "x")

		if got := installedBase(); got != "" {
			t.Errorf("installedBase = %q, want \"\" for a theme with no icon set", got)
		}
	})

	t.Run("non-Material-Black themes are ignored", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeFile(t, filepath.Join(home, ".themes/Colloid-Dark/gtk-3.0/gtk.css"), "x")

		if got := installedBase(); got != "" {
			t.Errorf("installedBase = %q, want \"\"", got)
		}
	})
}

func TestThemeBaseInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if themeBaseInstalled() {
		t.Error("themeBaseInstalled = true on an empty HOME")
	}
	installPair(t, home, "IceBlue")
	if !themeBaseInstalled() {
		t.Error("themeBaseInstalled = false with a complete pair on disk")
	}
}

func TestPaletteThemesBuilt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "Colloid-Dark", "#7fd8e8")
	palette(t, home, "ever", "Material-Black-Evergreen", "MB-Evergreen-Suru-GLOW", "Colloid-Dark", "#a7c080")

	if paletteThemesBuilt() {
		t.Error("paletteThemesBuilt = true with no assets on disk")
	}

	installPair(t, home, "IceBlue")
	if paletteThemesBuilt() {
		t.Error("paletteThemesBuilt = true with only one palette's assets built")
	}

	installPair(t, home, "Evergreen")
	if !paletteThemesBuilt() {
		t.Error("paletteThemesBuilt = false with every palette's assets on disk")
	}
}

func TestPaletteThemesBuiltWithNoPalettes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Nothing to build is trivially built, and must not report work pending
	// forever.
	if !paletteThemesBuilt() {
		t.Error("paletteThemesBuilt = false with no palettes installed")
	}
}

func TestPalettesSkipsUnparseableFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "good", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")
	writeFile(t, filepath.Join(home, ".config/theme/themes/broken.json"), `{"name":`)

	got := palettes()
	if len(got) != 1 || got[0].Name != "good" {
		t.Errorf("palettes = %d entries, want just the parseable one", len(got))
	}
}

// -- Colloid -----------------------------------------------------------------

func TestColloidFlags(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  []string
		fails bool
	}{
		{
			name: "with a tweak",
			in:   "Colloid-Green-Dark-Everforest",
			want: []string{"-t", "green", "-c", "dark", "--tweaks", "everforest"},
		},
		{
			name: "with a tweak, different palette",
			in:   "Colloid-Teal-Dark-Nord",
			want: []string{"-t", "teal", "-c", "dark", "--tweaks", "nord"},
		},
		// Upstream omits the accent when it is the default, so the lone segment
		// here is the colour variant, not an accent. Reading it as an accent
		// produced `-t dark`, which install.sh rejects — and because the failure
		// was only a printed "skipping", onedark's gtk4 apps were left unstyled
		// on any machine built from scratch.
		{
			name: "default accent, dark variant",
			in:   "Colloid-Dark",
			want: []string{"-c", "dark"},
		},
		{
			name: "accent with the standard variant",
			in:   "Colloid-Green",
			want: []string{"-t", "green", "-c", "standard"},
		},
		{
			name: "three parts",
			in:   "Colloid-Purple-Light",
			want: []string{"-t", "purple", "-c", "light"},
		},
		{
			name: "default accent with a tweak",
			in:   "Colloid-Dark-Nord",
			want: []string{"-c", "dark", "--tweaks", "nord"},
		},
		// The size suffix sits between the colour and the scheme in the name, so
		// reading segments positionally would pass it as a tweak.
		{
			name: "compact size",
			in:   "Colloid-Teal-Dark-Compact-Nord",
			want: []string{"-t", "teal", "-c", "dark", "-s", "compact", "--tweaks", "nord"},
		},
		{
			name: "size with the default accent",
			in:   "Colloid-Compact",
			want: []string{"-c", "standard", "-s", "compact"},
		},
		{name: "not a Colloid theme", in: "Material-Black-IceBlue", fails: true},
		{name: "empty", in: "", fails: true},
		{name: "prefix only", in: "Colloid", fails: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := colloidFlags(c.in)
			if c.fails {
				if ok {
					t.Errorf("colloidFlags(%q) = %v, want it rejected", c.in, got)
				}
				return
			}
			if !ok {
				t.Fatalf("colloidFlags(%q) was rejected", c.in)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("colloidFlags(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// The names the real palettes ask for must all be derivable, or installColloid
// silently skips them and the gtk4 apps fall out of palette.
func TestRealPaletteGTK4NamesAreDerivable(t *testing.T) {
	home := os.Getenv("HOME")
	if _, err := os.Stat(filepath.Join(home, ".config/theme/themes")); err != nil {
		t.Skip("no palettes installed")
	}
	names, err := theme.Names()
	if err != nil {
		t.Skip("cannot read palettes")
	}
	for _, n := range names {
		p, err := theme.Load(n)
		if err != nil {
			t.Fatalf("loading %s: %v", n, err)
		}
		if p.GTK.GTK4 == "" {
			continue
		}
		if _, ok := colloidFlags(p.GTK.GTK4); !ok {
			t.Errorf("palette %s asks for gtk4 theme %q, whose install flags cannot "+
				"be derived — installColloid would skip it", n, p.GTK.GTK4)
		}
	}
}

func TestColloidVariants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "Colloid-Teal-Dark-Nord", "#7fd8e8")
	palette(t, home, "ever", "Material-Black-Evergreen", "MB-Evergreen-Suru-GLOW", "Colloid-Green-Dark-Everforest", "#a7c080")
	// A third palette wanting the same gtk4 theme as the first.
	palette(t, home, "dup", "Material-Black-Dup", "MB-Dup-Suru-GLOW", "Colloid-Teal-Dark-Nord", "#111111")
	// And one wanting none at all.
	palette(t, home, "none", "Material-Black-None", "MB-None-Suru-GLOW", "", "#222222")

	got := colloidVariants()
	slices.Sort(got)
	want := []string{"Colloid-Green-Dark-Everforest", "Colloid-Teal-Dark-Nord"}
	if !slices.Equal(got, want) {
		t.Errorf("colloidVariants = %v, want %v (deduplicated, blanks dropped)", got, want)
	}
	if colloidInstalled() {
		t.Error("colloidInstalled = true with variants outstanding")
	}
}

func TestColloidVariantsSkipsInstalledThemes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "Colloid-Teal-Dark-Nord", "#7fd8e8")
	writeFile(t, filepath.Join(home, ".themes/Colloid-Teal-Dark-Nord/gtk-4.0/gtk.css"), "/* built */")

	if got := colloidVariants(); len(got) != 0 {
		t.Errorf("colloidVariants = %v, want none — it is already installed", got)
	}
	if !colloidInstalled() {
		t.Error("colloidInstalled = false with everything on disk")
	}
}

// install.sh creates the theme directory before compiling anything, so a run
// that failed part-way (no sassc, say) leaves a directory with no stylesheet.
// Treating that as installed strands the gtk4 apps unstyled forever, because
// renderGTK finds the theme but no gtk.css to link and quietly links nothing.
func TestColloidVariantsRejectsADirectoryWithNoStylesheet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "Colloid-Teal-Dark-Nord", "#7fd8e8")
	// Everything install.sh writes before it reaches the stylesheets.
	writeFile(t, filepath.Join(home, ".themes/Colloid-Teal-Dark-Nord/index.theme"),
		"[Desktop Entry]\nName=Colloid-Teal-Dark-Nord\n")
	mkdir(t, filepath.Join(home, ".themes/Colloid-Teal-Dark-Nord/gtk-4.0"))

	if got := colloidVariants(); len(got) != 1 {
		t.Errorf("colloidVariants = %v, want the half-built theme reported as outstanding", got)
	}
	if colloidInstalled() {
		t.Error("colloidInstalled = true for a theme directory containing no gtk.css")
	}
}

// The theme component has to install what Colloid's installer needs, or the
// step above fails silently on a fresh machine.
func TestThemeComponentInstallsColloidBuildDependencies(t *testing.T) {
	var pkgs []string
	for _, c := range catalog() {
		if c.Key == "theme" {
			pkgs = c.Packages
		}
	}
	for _, need := range []string{"sassc", "gtk-engine-murrine"} {
		if !slices.Contains(pkgs, need) {
			t.Errorf("the theme component does not install %q, which Colloid's "+
				"install.sh needs; without it the gtk4 themes build empty", need)
		}
	}
}

func TestBuildPaletteThemesNeedsABase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")

	err := buildPaletteThemes()
	if err == nil {
		t.Fatal("buildPaletteThemes with no base returned nil error")
	}
	if !strings.Contains(err.Error(), "Material-Black") {
		t.Errorf("error %q does not explain what is missing", err)
	}
}

// buildPaletteThemes derives every palette that is not already built, from
// whichever pair happens to be installed.
func TestBuildPaletteThemesDerivesTheMissingOnes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installPair(t, home, "IceBlue")
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")
	palette(t, home, "ever", "Material-Black-Evergreen", "MB-Evergreen-Suru-GLOW", "", "#a7c080")

	// The recolour reads the base's gradient out of folder.svg, so it needs the
	// oomox structure rather than a stub.
	writeFile(t, filepath.Join(home, ".local/share/icons/MB-IceBlue-Suru-GLOW/places/scalable/folder.svg"),
		`<svg><linearGradient id="oomox">`+
			`<stop style="stop-color:#00e5ce"/><stop style="stop-color:#007267"/>`+
			`</linearGradient><path fill="url(#oomox)"/></svg>`)

	if err := buildPaletteThemes(); err != nil {
		t.Fatalf("buildPaletteThemes: %v", err)
	}

	for _, p := range []string{
		".themes/Material-Black-Evergreen/gtk-3.0/gtk.css",
		".local/share/icons/MB-Evergreen-Suru-GLOW/places/scalable/folder.svg",
	} {
		if _, err := os.Stat(filepath.Join(home, p)); err != nil {
			t.Errorf("%s was not derived: %v", p, err)
		}
	}
	if !paletteThemesBuilt() {
		t.Error("paletteThemesBuilt = false after building")
	}
}

func TestBuildPaletteThemesSkipsPalettesWithNoAccent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installPair(t, home, "IceBlue")
	writeFile(t, filepath.Join(home, ".local/share/icons/MB-IceBlue-Suru-GLOW/places/scalable/folder.svg"),
		`<svg><linearGradient id="oomox">`+
			`<stop style="stop-color:#00e5ce"/><stop style="stop-color:#007267"/>`+
			`</linearGradient><path fill="url(#oomox)"/></svg>`)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")
	palette(t, home, "broken", "Material-Black-Broken", "MB-Broken-Suru-GLOW", "", "")

	// A palette with no accent is skipped with a note rather than aborting the
	// rest of the build.
	if err := buildPaletteThemes(); err != nil {
		t.Fatalf("buildPaletteThemes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".themes/Material-Black-Broken")); err == nil {
		t.Error("a palette with no accent colour was built anyway")
	}
}
