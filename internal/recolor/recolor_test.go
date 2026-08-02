package recolor

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The synthetic base mirrors the shape recolor reads: a GTK theme whose accent
// is simply its most-repeated hex, and an icon set whose fill gradient names
// itself so the two stops can be read rather than guessed at by position.

const (
	baseAccent = "#00e5ce"
	baseLight  = "#00e5ce"
	baseDark   = "#007267"
)

// fakeBase installs a Material-Black + Suru-GLOW pair under a temporary HOME
// and returns the variant stem.
func fakeBase(t *testing.T, variant string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	installBase(t, variant)
	return variant
}

func installBase(t *testing.T, variant string) {
	t.Helper()
	gtk := filepath.Join(os.Getenv("HOME"), ".themes", "Material-Black-"+variant)
	icons := filepath.Join(os.Getenv("HOME"), ".local/share/icons", "MB-"+variant+"-Suru-GLOW")

	// The accent wins on count: three uses against two for the decoy. Mixed
	// case, as upstream really is.
	write(t, filepath.Join(gtk, "gtk-3.0", "gtk.css"), strings.Join([]string{
		"a { color: " + baseAccent + "; }",
		"b { color: #00E5CE; }",
		"c { border-color: " + baseAccent + "; }",
		"d { color: #112233; }",
		"e { color: #112233; }",
		"f { color: " + scaled(RGB{0, 229, 206}, 0.8) + "; }",
	}, "\n"))
	write(t, filepath.Join(gtk, "index.theme"),
		"[Desktop Entry]\nName=Material-Black-"+variant+"\nComment=Material-Black-"+variant+" theme\n")
	write(t, filepath.Join(gtk, "gtk-2.0", "main.rc"), "bg[SELECTED] = \""+baseAccent+"\"\n")
	// Extensionless, so it falls outside textSuffixes — see the test below.
	write(t, filepath.Join(gtk, "gtk-2.0", "gtkrc"),
		"gtk-color-scheme = \"selected_bg_color:"+baseAccent+"\"\n")
	// A file type the recolour must leave alone.
	write(t, filepath.Join(gtk, "README.md"), "accent is "+baseAccent+"\n")

	write(t, filepath.Join(icons, "places", "scalable", "folder.svg"), iconSVG())
	write(t, filepath.Join(icons, "apps", "scalable", "app.svg"), iconSVG())
	write(t, filepath.Join(icons, "index.theme"),
		"[Icon Theme]\nName=MB-"+variant+"-Suru-GLOW\nComment=Suru GLOW icons\nDirectories=places/scalable\n")
}

func iconSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg">
  <linearGradient id="decoy"><stop stop-color:#ff0000"/></linearGradient>
  <linearGradient id="oomox_gradient">
    <stop offset="0" style="stop-color:` + baseLight + `"/>
    <stop offset="1" style="stop-color:` + baseDark + `"/>
  </linearGradient>
  <path fill="url(#oomox_gradient)" d="M0 0h10v10H0z"/>
</svg>`
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// -- readBase ----------------------------------------------------------------

func TestReadBase(t *testing.T) {
	fakeBase(t, "Teal")

	got, err := readBase("Teal")
	if err != nil {
		t.Fatalf("readBase: %v", err)
	}
	if got.accent != baseAccent {
		t.Errorf("accent = %q, want %q (the most-repeated colour in the sheet)", got.accent, baseAccent)
	}
	if got.light != baseLight || got.dark != baseDark {
		t.Errorf("stops = (%q, %q), want (%q, %q)", got.light, got.dark, baseLight, baseDark)
	}
}

func TestReadBaseLowercasesMixedCaseAccents(t *testing.T) {
	fakeBase(t, "Teal")
	// Upstream writes the same colour both ways; they must count as one.
	write(t, filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-Teal/gtk-3.0/gtk.css"),
		"a{color:#00E5CE}b{color:#00e5ce}c{color:#112233}")

	got, err := readBase("Teal")
	if err != nil {
		t.Fatalf("readBase: %v", err)
	}
	if got.accent != "#00e5ce" {
		t.Errorf("accent = %q, want %q — case variants must be counted together", got.accent, "#00e5ce")
	}
}

// A tie goes to whichever appeared first, which is what Python's max() over an
// insertion-ordered dict did. Without this the accent would flip between runs.
func TestReadBaseBreaksTiesByFirstSeen(t *testing.T) {
	fakeBase(t, "Teal")
	write(t, filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-Teal/gtk-3.0/gtk.css"),
		"a{color:#aaaaaa}b{color:#bbbbbb}c{color:#aaaaaa}d{color:#bbbbbb}")

	for range 20 {
		got, err := readBase("Teal")
		if err != nil {
			t.Fatal(err)
		}
		if got.accent != "#aaaaaa" {
			t.Fatalf("accent = %q, want #aaaaaa (first seen wins a tie)", got.accent)
		}
	}
}

func TestReadBaseFallsBackToTheDefaultGradientID(t *testing.T) {
	fakeBase(t, "Teal")
	// No fill="url(#...)" at all, so the lookup falls back to "oomox".
	write(t, filepath.Join(os.Getenv("HOME"), ".local/share/icons/MB-Teal-Suru-GLOW/places/scalable/folder.svg"),
		`<svg><linearGradient id="oomox">`+
			`<stop style="stop-color:`+baseLight+`"/><stop style="stop-color:`+baseDark+`"/>`+
			`</linearGradient></svg>`)

	got, err := readBase("Teal")
	if err != nil {
		t.Fatalf("readBase: %v", err)
	}
	if got.light != baseLight || got.dark != baseDark {
		t.Errorf("stops = (%q, %q), want (%q, %q)", got.light, got.dark, baseLight, baseDark)
	}
}

func TestReadBaseErrors(t *testing.T) {
	t.Run("nothing installed", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if _, err := readBase("Absent"); err == nil {
			t.Error("readBase of a missing base returned nil error")
		}
	})

	t.Run("GTK theme without the icon set", func(t *testing.T) {
		fakeBase(t, "Teal")
		if err := os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".local/share/icons")); err != nil {
			t.Fatal(err)
		}
		if _, err := readBase("Teal"); err == nil {
			t.Error("readBase with no icon set returned nil error — both halves are needed")
		}
	})

	t.Run("no colours in the sheet", func(t *testing.T) {
		fakeBase(t, "Teal")
		write(t, filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-Teal/gtk-3.0/gtk.css"), "/* empty */")
		if _, err := readBase("Teal"); err == nil {
			t.Error("readBase with no colours returned nil error")
		}
	})

	t.Run("gradient with too few stops", func(t *testing.T) {
		fakeBase(t, "Teal")
		write(t, filepath.Join(os.Getenv("HOME"), ".local/share/icons/MB-Teal-Suru-GLOW/places/scalable/folder.svg"),
			`<svg><linearGradient id="oomox"><stop style="stop-color:#00e5ce"/></linearGradient>`+
				`<path fill="url(#oomox)"/></svg>`)
		if _, err := readBase("Teal"); err == nil {
			t.Error("readBase with one stop returned nil error")
		}
	})
}

// -- recolourPNG -------------------------------------------------------------

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func readPNG(t *testing.T, path string) *image.NRGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return asNRGBA(img)
}

func TestRecolourPNG(t *testing.T) {
	source := RGB{0, 229, 206}  // teal
	target := RGB{232, 100, 60} // orange

	src := image.NewNRGBA(image.Rect(0, 0, 6, 1))
	pixels := []color.NRGBA{
		{R: 0, G: 229, B: 206, A: 255},   // 0: the accent itself
		{R: 0, G: 114, B: 103, A: 255},   // 1: a darker shade of the accent
		{R: 128, G: 128, B: 128, A: 255}, // 2: grey chrome — must survive
		{R: 220, G: 40, B: 40, A: 255},   // 3: a different hue — must survive
		{R: 0, G: 0, B: 0, A: 0},         // 4: fully transparent — skipped
		{R: 0, G: 229, B: 206, A: 3},     // 5: accent at very low alpha
	}
	for i, p := range pixels {
		src.SetNRGBA(i, 0, p)
	}

	path := filepath.Join(t.TempDir(), "check.png")
	writePNG(t, path, src)
	if err := recolourPNG(path, source, target); err != nil {
		t.Fatalf("recolourPNG: %v", err)
	}
	out := readPNG(t, path)

	t.Run("accent pixel takes the target hue", func(t *testing.T) {
		got := out.NRGBAAt(0, 0)
		if got == pixels[0] {
			t.Fatal("the accent pixel was not recoloured")
		}
		h, _, _ := rgbToHLS(float64(got.R)/255, float64(got.G)/255, float64(got.B)/255)
		wantH, _, _ := rgbToHLS(float64(target[0])/255, float64(target[1])/255, float64(target[2])/255)
		if diff := abs(h - wantH); diff > 0.01 {
			t.Errorf("hue = %v, want ~%v", h, wantH)
		}
	})

	t.Run("the darker shade stays darker", func(t *testing.T) {
		_, l0, _ := rgbToHLS(chan3(out.NRGBAAt(0, 0)))
		_, l1, _ := rgbToHLS(chan3(out.NRGBAAt(1, 0)))
		if l1 >= l0 {
			t.Errorf("shade lightness %v is not below the accent's %v — relative shading was lost", l1, l0)
		}
	})

	t.Run("grey chrome is untouched", func(t *testing.T) {
		if got := out.NRGBAAt(2, 0); got != pixels[2] {
			t.Errorf("grey pixel = %+v, want %+v — saturation below 0.15 must be skipped", got, pixels[2])
		}
	})

	t.Run("a different hue is untouched", func(t *testing.T) {
		if got := out.NRGBAAt(3, 0); got != pixels[3] {
			t.Errorf("red pixel = %+v, want %+v — only the accent's hue family is remapped", got, pixels[3])
		}
	})

	t.Run("transparent pixels are skipped", func(t *testing.T) {
		if got := out.NRGBAAt(4, 0); got.A != 0 {
			t.Errorf("transparent pixel gained alpha %d", got.A)
		}
	})

	// The premultiplication bug showed up exactly here: a low-alpha accent pixel
	// round-tripped through At() loses almost all colour precision.
	t.Run("low alpha keeps its alpha and is still recoloured", func(t *testing.T) {
		got := out.NRGBAAt(5, 0)
		if got.A != 3 {
			t.Errorf("alpha = %d, want 3 preserved exactly", got.A)
		}
		if got.R == pixels[5].R && got.G == pixels[5].G && got.B == pixels[5].B {
			t.Error("the low-alpha accent pixel was not recoloured")
		}
		// It must land on the same colour as the fully opaque accent pixel:
		// alpha has no bearing on the hue mapping.
		opaque := out.NRGBAAt(0, 0)
		if got.R != opaque.R || got.G != opaque.G || got.B != opaque.B {
			t.Errorf("low-alpha pixel = (%d,%d,%d) but the opaque one = (%d,%d,%d); "+
				"alpha must not affect the recolour",
				got.R, got.G, got.B, opaque.R, opaque.G, opaque.B)
		}
	})
}

// An image with nothing to recolour must be left byte-identical rather than
// re-encoded, since re-encoding would churn every unrelated PNG in the theme.
func TestRecolourPNGLeavesUntouchedFilesAlone(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{R: 20, G: 20, B: 20, A: 255})

	path := filepath.Join(t.TempDir(), "grey.png")
	writePNG(t, path, src)
	before := read(t, path)

	if err := recolourPNG(path, RGB{0, 229, 206}, RGB{232, 100, 60}); err != nil {
		t.Fatalf("recolourPNG: %v", err)
	}
	if after := read(t, path); after != before {
		t.Error("a PNG with no accent pixels was rewritten; it should be left alone")
	}
}

func TestRecolourPNGIgnoresUndecodableFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notreally.png")
	write(t, path, "this is not a PNG")

	// Not an error: the walk hits whatever a theme happens to ship.
	if err := recolourPNG(path, RGB{0, 229, 206}, RGB{232, 100, 60}); err != nil {
		t.Errorf("recolourPNG on a non-PNG returned %v, want nil", err)
	}
	if got := read(t, path); got != "this is not a PNG" {
		t.Errorf("file was modified: %q", got)
	}
}

func TestRecolourPNGPreservesFileMode(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 229, B: 206, A: 255})

	path := filepath.Join(t.TempDir(), "m.png")
	writePNG(t, path, src)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	if err := recolourPNG(path, RGB{0, 229, 206}, RGB{232, 100, 60}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640 preserved", fi.Mode().Perm())
	}
}

func TestRecolourPNGMissingFile(t *testing.T) {
	err := recolourPNG(filepath.Join(t.TempDir(), "nope.png"), RGB{0, 229, 206}, RGB{1, 2, 3})
	if err == nil {
		t.Error("recolourPNG on a missing file returned nil error")
	}
}

// -- buildGTK / buildIcons ---------------------------------------------------

func TestBuildGTK(t *testing.T) {
	fakeBase(t, "Teal")
	accent := RGB{127, 216, 232} // ice blue

	// A PNG carrying the accent, as the GTK2 checkboxes do.
	png1 := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	png1.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 229, B: 206, A: 255})
	writePNG(t, filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-Teal/gtk-2.0/assets/check.png"), png1)

	dest, err := buildGTK("Teal", "IceBlue", accent)
	if err != nil {
		t.Fatalf("buildGTK: %v", err)
	}
	if want := filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-IceBlue"); dest != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}

	t.Run("accent and hover shade are substituted in CSS", func(t *testing.T) {
		css := read(t, filepath.Join(dest, "gtk-3.0", "gtk.css"))
		if strings.Contains(strings.ToLower(css), baseAccent) {
			t.Errorf("the base accent survives in the derived sheet:\n%s", css)
		}
		if !strings.Contains(css, "#7fd8e8") {
			t.Errorf("the new accent is absent:\n%s", css)
		}
		// The hover/pressed shade is mapped as its own entry.
		if !strings.Contains(css, scaled(accent, 0.8)) {
			t.Errorf("the hover shade %s is absent:\n%s", scaled(accent, 0.8), css)
		}
	})

	t.Run("other listed text formats are rewritten too", func(t *testing.T) {
		if got := read(t, filepath.Join(dest, "gtk-2.0", "main.rc")); !strings.Contains(got, "#7fd8e8") {
			t.Errorf("main.rc not recoloured: %q", got)
		}
	})

	t.Run("unlisted file types are left alone", func(t *testing.T) {
		if got := read(t, filepath.Join(dest, "README.md")); !strings.Contains(got, baseAccent) {
			t.Errorf("README.md was rewritten: %q — only the listed suffixes should be", got)
		}
	})

	t.Run("PNG assets are recoloured", func(t *testing.T) {
		got := readPNG(t, filepath.Join(dest, "gtk-2.0/assets/check.png")).NRGBAAt(0, 0)
		if got.R == 0 && got.G == 229 && got.B == 206 {
			t.Error("the GTK2 PNG kept the base accent")
		}
	})

	t.Run("index.theme is renamed to match the directory", func(t *testing.T) {
		got := read(t, filepath.Join(dest, "index.theme"))
		if !strings.Contains(got, "Name=Material-Black-IceBlue") {
			t.Errorf("index.theme not renamed:\n%s", got)
		}
		if strings.Contains(got, "Material-Black-Teal") {
			t.Errorf("index.theme still names the base:\n%s", got)
		}
	})

	t.Run("the base is left untouched", func(t *testing.T) {
		css := read(t, filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-Teal/gtk-3.0/gtk.css"))
		if !strings.Contains(strings.ToLower(css), baseAccent) {
			t.Error("buildGTK modified the base theme")
		}
	})
}

// Known gap, pinned deliberately rather than asserted as correct.
//
// textSuffixes matches on the file extension, and GTK2's main entry point is
// named plain "gtkrc" with no extension at all — so its selected_bg_color and
// link_color keep whatever base the theme was derived from. On this machine
// that is visible: Material-Black-IceBlue's gtk.css says #7fd8e8 while its
// gtkrc still says #38b7ab.
//
// recolor.py had the identical suffix tuple and Python's Path("gtkrc").suffix
// is likewise "", so this is inherited, not a porting regression. It only
// affects GTK2 apps, of which this desktop has almost none. Change the test
// with the behaviour if that ever stops being true.
func TestBuildGTKDoesNotRecolourExtensionlessGtkrc(t *testing.T) {
	fakeBase(t, "Teal")

	dest, err := buildGTK("Teal", "IceBlue", RGB{127, 216, 232})
	if err != nil {
		t.Fatalf("buildGTK: %v", err)
	}
	got := read(t, filepath.Join(dest, "gtk-2.0", "gtkrc"))
	if !strings.Contains(got, baseAccent) {
		t.Errorf("gtkrc = %q — it now gets recoloured, so this known gap is fixed "+
			"and the test should assert the new behaviour", got)
	}
}

func TestBuildGTKReplacesAnExistingDestination(t *testing.T) {
	fakeBase(t, "Teal")
	dest := filepath.Join(os.Getenv("HOME"), ".themes", "Material-Black-IceBlue")
	write(t, filepath.Join(dest, "stale.css"), "left over from a previous build")

	if _, err := buildGTK("Teal", "IceBlue", RGB{127, 216, 232}); err != nil {
		t.Fatalf("buildGTK: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.css")); !os.IsNotExist(err) {
		t.Error("a stale file from an earlier build survived; the destination must be replaced wholesale")
	}
}

func TestBuildGTKMissingBase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := buildGTK("Nope", "X", RGB{1, 2, 3}); err == nil {
		t.Error("buildGTK with no base returned nil error")
	}
}

func TestBuildIcons(t *testing.T) {
	fakeBase(t, "Teal")
	accent := RGB{127, 216, 232}

	// A symlinked icon must not be rewritten — it follows whatever it points at,
	// and rewriting through it would break the link or duplicate the work.
	iconDir := filepath.Join(os.Getenv("HOME"), ".local/share/icons/MB-Teal-Suru-GLOW/places/scalable")
	if err := os.Symlink("folder.svg", filepath.Join(iconDir, "folder-open.svg")); err != nil {
		t.Fatal(err)
	}

	dest, count, err := buildIcons("Teal", "IceBlue", accent)
	if err != nil {
		t.Fatalf("buildIcons: %v", err)
	}
	if want := filepath.Join(os.Getenv("HOME"), ".local/share/icons/MB-IceBlue-Suru-GLOW"); dest != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	// Two real SVGs, and the symlink is not counted.
	if count != 2 {
		t.Errorf("count = %d, want 2 (real SVGs only, symlinks excluded)", count)
	}

	t.Run("both gradient stops are remapped", func(t *testing.T) {
		svg := read(t, filepath.Join(dest, "places", "scalable", "folder.svg"))
		if !strings.Contains(svg, "#7fd8e8") {
			t.Errorf("light stop not remapped:\n%s", svg)
		}
		if want := scaled(accent, 0.5); !strings.Contains(svg, want) {
			t.Errorf("dark stop %s not remapped:\n%s", want, svg)
		}
		if strings.Contains(svg, baseDark) {
			t.Errorf("the base dark stop survives:\n%s", svg)
		}
	})

	t.Run("symlinks stay symlinks", func(t *testing.T) {
		link, err := os.Readlink(filepath.Join(dest, "places", "scalable", "folder-open.svg"))
		if err != nil {
			t.Fatalf("symlink not preserved: %v", err)
		}
		if link != "folder.svg" {
			t.Errorf("symlink target = %q, want %q", link, "folder.svg")
		}
	})

	t.Run("index.theme is renamed and gains Inherits", func(t *testing.T) {
		got := read(t, filepath.Join(dest, "index.theme"))
		if !strings.Contains(got, "Name=MB-IceBlue-Suru-GLOW") {
			t.Errorf("index.theme not renamed:\n%s", got)
		}
		if !strings.Contains(got, "Inherits=Papirus-Dark,Papirus,hicolor") {
			t.Errorf("Inherits not added:\n%s", got)
		}
	})
}

func TestBuildIconsKeepsAnExistingInheritsLine(t *testing.T) {
	fakeBase(t, "Teal")
	write(t, filepath.Join(os.Getenv("HOME"), ".local/share/icons/MB-Teal-Suru-GLOW/index.theme"),
		"[Icon Theme]\nName=MB-Teal-Suru-GLOW\nComment=x\nInherits=Adwaita\n")

	dest, _, err := buildIcons("Teal", "IceBlue", RGB{127, 216, 232})
	if err != nil {
		t.Fatalf("buildIcons: %v", err)
	}
	got := read(t, filepath.Join(dest, "index.theme"))
	if strings.Count(got, "Inherits=") != 1 {
		t.Errorf("expected exactly one Inherits line, got:\n%s", got)
	}
	if !strings.Contains(got, "Inherits=Adwaita") {
		t.Errorf("an existing Inherits line was replaced:\n%s", got)
	}
}

func TestBuildIconsMissingBase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, _, err := buildIcons("Nope", "X", RGB{1, 2, 3}); err == nil {
		t.Error("buildIcons with no base returned nil error")
	}
}

// -- Run ---------------------------------------------------------------------

func TestRun(t *testing.T) {
	fakeBase(t, "Teal")

	if err := Run("Teal", "#7fd8e8", "IceBlue"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range []string{
		".themes/Material-Black-IceBlue/gtk-3.0/gtk.css",
		".local/share/icons/MB-IceBlue-Suru-GLOW/places/scalable/folder.svg",
	} {
		if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), p)); err != nil {
			t.Errorf("%s was not produced: %v", p, err)
		}
	}
}

// The point of reading the accent back off disk: a derived build can seed the
// next one, so the upstream base need not stay installed.
func TestRunCanChainFromItsOwnOutput(t *testing.T) {
	fakeBase(t, "Teal")

	if err := Run("Teal", "#7fd8e8", "IceBlue"); err != nil {
		t.Fatalf("first build: %v", err)
	}
	// Drop the original, exactly as the real machine has.
	if err := os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-Teal")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".local/share/icons/MB-Teal-Suru-GLOW")); err != nil {
		t.Fatal(err)
	}

	if err := Run("IceBlue", "#7fd888", "Evergreen"); err != nil {
		t.Fatalf("chained build: %v", err)
	}
	css := read(t, filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-Evergreen/gtk-3.0/gtk.css"))
	if !strings.Contains(css, "#7fd888") {
		t.Errorf("the chained build did not apply the new accent:\n%s", css)
	}
	if strings.Contains(css, "#7fd8e8") {
		t.Errorf("the intermediate accent survives:\n%s", css)
	}
}

func TestRunRejectsABadColour(t *testing.T) {
	fakeBase(t, "Teal")
	if err := Run("Teal", "not-a-colour", "X"); err == nil {
		t.Error("Run with an invalid colour returned nil error")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".themes/Material-Black-X")); err == nil {
		t.Error("Run built a theme despite the colour being rejected")
	}
}

// -- helpers -----------------------------------------------------------------

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func chan3(c color.NRGBA) (float64, float64, float64) {
	return float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255
}
