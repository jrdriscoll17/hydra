package recolor

// Build a Material-Black + Suru-GLOW pair in an arbitrary accent colour.
//
//	setup recolor <base-variant> <#hex> <name>
//	setup recolor Pistachio '#7fd8e8' IceBlue
//
// Produces ~/.themes/Material-Black-<name> and
// ~/.local/share/icons/MB-<name>-Suru-GLOW, leaving the base alone.
//
// Both halves are coloured mechanically rather than drawn:
//
//   - the GTK theme states its accent as a single hex (81 times in gtk.css, plus
//     a handful of SVG assets and 35 GTK2 PNGs — checkboxes, radios, switches),
//   - every icon is tinted by one two-stop linear gradient, light to dark. That
//     pair is what differs between Blueberry, Lime, Pistachio and the rest, so
//     swapping it recolours all ~25k icons at once.
//
// The PNGs are recoloured by mapping each accent-ish pixel through the same
// lightness ramp, which keeps the anti-aliased edges intact.
//
// Ported from the original recolor.py. The colour maths mirrors Python's
// colorsys, including its round-half-to-even, so a rebuild produces the same
// bytes as the Python version did.

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// -- colour maths (ports of Python's colorsys) -------------------------------

func rgbToHLS(r, g, b float64) (h, l, s float64) {
	maxc := math.Max(r, math.Max(g, b))
	minc := math.Min(r, math.Min(g, b))
	sumc, rangec := maxc+minc, maxc-minc
	l = sumc / 2
	if minc == maxc {
		return 0, l, 0
	}
	if l <= 0.5 {
		s = rangec / sumc
	} else {
		s = rangec / (2 - maxc - minc)
	}
	rc, gc, bc := (maxc-r)/rangec, (maxc-g)/rangec, (maxc-b)/rangec
	switch {
	case r == maxc:
		h = bc - gc
	case g == maxc:
		h = 2 + rc - bc
	default:
		h = 4 + gc - rc
	}
	h = math.Mod(h/6, 1)
	if h < 0 {
		h++ // Go's Mod keeps the sign; Python's % does not.
	}
	return h, l, s
}

func hlsToRGB(h, l, s float64) (r, g, b float64) {
	if s == 0 {
		return l, l, l
	}
	var m2 float64
	if l <= 0.5 {
		m2 = l * (1 + s)
	} else {
		m2 = l + s - l*s
	}
	m1 := 2*l - m2
	return hlsValue(m1, m2, h+1.0/3), hlsValue(m1, m2, h), hlsValue(m1, m2, h-1.0/3)
}

func hlsValue(m1, m2, hue float64) float64 {
	hue = math.Mod(hue, 1)
	if hue < 0 {
		hue++
	}
	switch {
	case hue < 1.0/6:
		return m1 + (m2-m1)*hue*6
	case hue < 0.5:
		return m2
	case hue < 2.0/3:
		return m1 + (m2-m1)*(2.0/3-hue)*6
	}
	return m1
}

// RGB is an 8-bit colour.
type RGB [3]int

func parseHex(value string) (RGB, error) {
	v := strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(v) != 6 {
		return RGB{}, fmt.Errorf("not a colour: #%s", v)
	}
	var out RGB
	for i := range 3 {
		n, err := strconv.ParseUint(v[i*2:i*2+2], 16, 8)
		if err != nil {
			return RGB{}, fmt.Errorf("not a colour: #%s", v)
		}
		out[i] = int(n)
	}
	return out, nil
}

// clamp8 rounds half-to-even, matching Python's round(), then clamps.
func clamp8(c float64) int {
	return min(max(int(math.RoundToEven(c)), 0), 255)
}

func toHex(r, g, b float64) string {
	return fmt.Sprintf("#%02x%02x%02x", clamp8(r), clamp8(g), clamp8(b))
}

// scaled returns a darker (or lighter) shade of the same hue, matching how
// upstream picks its second gradient stop — roughly half the lightness.
func scaled(c RGB, factor float64) string {
	h, l, s := rgbToHLS(float64(c[0])/255, float64(c[1])/255, float64(c[2])/255)
	r, g, b := hlsToRGB(h, math.Max(0, math.Min(1, l*factor)), s)
	return toHex(r*255, g*255, b*255)
}

// -- reading a base ----------------------------------------------------------

type baseColours struct{ accent, light, dark string }

var (
	hexRe      = regexp.MustCompile(`#[0-9a-fA-F]{6}`)
	fillRe     = regexp.MustCompile(`fill="url\(#([^)]+)\)"`)
	stopRe     = regexp.MustCompile(`stop-color:\s*(#[0-9a-fA-F]{6})`)
	nameRe     = regexp.MustCompile(`(?m)^Name=.*$`)
	inheritsRe = regexp.MustCompile(`(?m)^Inherits=`)
	commentRe  = regexp.MustCompile(`(?m)^Comment=.*$`)
)

// readBase reads a base's accent and gradient stops back off the installed
// theme. Nothing about the upstream variants is hardcoded, so a recoloured
// build can itself be the base for the next one and the originals need not stay
// on disk.
func readBase(base string) (baseColours, error) {
	gtk := sys.InHome(filepath.Join(".themes", "Material-Black-"+base, "gtk-3.0", "gtk.css"))
	icon := sys.InHome(filepath.Join(".local/share/icons", "MB-"+base+"-Suru-GLOW",
		"places", "scalable", "folder.svg"))
	if !sys.Exists(gtk) || !sys.Exists(icon) {
		return baseColours{}, fmt.Errorf(
			"%s is not installed (need both the GTK theme and the icon set)", base)
	}

	sheet, err := os.ReadFile(gtk)
	if err != nil {
		return baseColours{}, err
	}
	// The accent is simply the most-repeated colour in the sheet — 81 uses,
	// against ~50 for the next one. Ties go to whichever appeared first, which
	// is what Python's max() over an insertion-ordered dict does.
	counts := map[string]int{}
	var order []string
	for _, m := range hexRe.FindAllString(string(sheet), -1) {
		l := strings.ToLower(m)
		if _, seen := counts[l]; !seen {
			order = append(order, l)
		}
		counts[l]++
	}
	if len(order) == 0 {
		return baseColours{}, fmt.Errorf("no colours found in %s", gtk)
	}
	accent := order[0]
	for _, h := range order {
		if counts[h] > counts[accent] {
			accent = h
		}
	}

	// Every icon carries two dozen gradients, all identical across variants
	// except the one the artwork actually fills with. These sets were generated
	// with oomox, which names that one — so read its two stops rather than
	// guessing by position.
	svgBytes, err := os.ReadFile(icon)
	if err != nil {
		return baseColours{}, err
	}
	svg := string(svgBytes)
	gradientID := "oomox"
	if m := fillRe.FindStringSubmatch(svg); m != nil {
		gradientID = m[1]
	}
	blockRe, err := regexp.Compile(
		`(?s)<linearGradient[^>]*id="` + regexp.QuoteMeta(gradientID) + `"[^>]*>(.*?)</linearGradient>`)
	if err != nil {
		return baseColours{}, err
	}
	var stops []string
	if block := blockRe.FindStringSubmatch(svg); block != nil {
		for _, m := range stopRe.FindAllStringSubmatch(block[1], -1) {
			stops = append(stops, strings.ToLower(m[1]))
		}
	}
	if len(stops) < 2 {
		return baseColours{}, fmt.Errorf(
			"could not read the %q gradient out of %s", gradientID, icon)
	}
	return baseColours{accent: accent, light: stops[0], dark: stops[1]}, nil
}

// Accent reports the accent colour an installed variant was built in, as a
// lowercase "#rrggbb".
//
// The build is only ever derived from a palette, never recorded against it, so
// the theme on disk is the only statement of which accent it carries. Setup
// compares this with the palette to notice that a palette's accent has moved
// since the theme was derived — a rebuild it would otherwise skip, because the
// directory exists and nothing else distinguishes a stale one.
func Accent(variant string) (string, error) {
	colours, err := readBase(variant)
	if err != nil {
		return "", err
	}
	return colours.accent, nil
}

// SameColour reports whether two hex colours are the same, ignoring case and a
// leading "#". Palettes are hand-edited, so "#67DBEF" and "67dbef" both turn up.
func SameColour(a, b string) bool {
	pa, err := parseHex(a)
	if err != nil {
		return false
	}
	pb, err := parseHex(b)
	if err != nil {
		return false
	}
	return pa == pb
}

// -- rewriting ---------------------------------------------------------------

// recolourText does case-insensitive hex substitution; upstream mixes #00E5CE
// and #00e5ce.
func recolourText(text string, mapping map[string]string) (string, bool) {
	changed := false
	for old, new := range mapping {
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(old))
		if re.MatchString(text) {
			text = re.ReplaceAllLiteralString(text, new)
			changed = true
		}
	}
	return text, changed
}

// recolourPNG re-tints accent-coloured pixels, keeping their relative lightness
// so anti-aliased edges and the pressed/hover shades survive.
func recolourPNG(path string, source, target RGB) error {
	srcH, srcL, _ := rgbToHLS(float64(source[0])/255, float64(source[1])/255, float64(source[2])/255)
	tgtH, tgtL, tgtS := rgbToHLS(float64(target[0])/255, float64(target[1])/255, float64(target[2])/255)

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		return nil // not a PNG we can read; leave it alone
	}

	// Work on non-premultiplied pixels. Going through image.Image.At() would
	// premultiply by alpha and force a lossy divide back, which visibly damages
	// anti-aliased edges and anything semi-transparent.
	dst := asNRGBA(src)

	bounds := dst.Bounds()
	touched := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			px := dst.NRGBAAt(x, y)
			if px.A == 0 {
				continue
			}
			r, g, b := float64(px.R)/255, float64(px.G)/255, float64(px.B)/255

			h, l, s := rgbToHLS(r, g, b)
			delta := math.Abs(h - srcH)
			// Greys are chrome, not accent; and only the accent's own hue
			// family gets remapped.
			if s < 0.15 || math.Min(delta, 1-delta) > 0.08 {
				continue
			}
			ratio := 1.0
			if srcL != 0 {
				ratio = tgtL / srcL
			}
			nr, ng, nb := hlsToRGB(tgtH, l*ratio, tgtS)
			dst.SetNRGBA(x, y, nrgba(clamp8(nr*255), clamp8(ng*255), clamp8(nb*255), int(px.A)))
			touched = true
		}
	}
	if !touched {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, dst)
}

// asNRGBA returns the image as *image.NRGBA, reusing it when the decoder
// already produced one so no conversion happens at all.
func asNRGBA(src image.Image) *image.NRGBA {
	if n, ok := src.(*image.NRGBA); ok {
		return n
	}
	b := src.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA))
		}
	}
	return out
}

func nrgba(r, g, b, a int) color.NRGBA {
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}
}

// -- building ----------------------------------------------------------------

var textSuffixes = map[string]bool{
	".css": true, ".svg": true, ".rc": true,
	".xml": true, ".themerc": true, ".theme": true,
}

// staging is where a build is assembled before it replaces dest.
//
// Building straight into dest cannot express base == name, and that is the
// case that matters most: re-deriving an installed theme in a new accent. The
// old code removed dest and then copied src into it, so recolouring a theme in
// place deleted its own source. Assembling beside dest and renaming at the end
// leaves src untouched until there is a complete build to swap in, which also
// means a failure half way through no longer leaves a partly recoloured theme
// installed.
func staging(dest string) string { return dest + ".building" }

// swap moves a finished build over dest. Same parent directory, so the rename
// is atomic and cannot leave both.
func swap(staged, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(staged, dest)
}

func buildGTK(base, name string, accent RGB) (string, error) {
	src := sys.InHome(filepath.Join(".themes", "Material-Black-"+base))
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not installed", src)
	}

	// Read the base before copying anything: a base that cannot be read should
	// fail before the ~150MB tree walk, not after it.
	colours, err := readBase(base)
	if err != nil {
		return "", err
	}
	oldAccent, err := parseHex(colours.accent)
	if err != nil {
		return "", err
	}

	dest := sys.InHome(filepath.Join(".themes", "Material-Black-"+name))
	staged := staging(dest)
	defer os.RemoveAll(staged) // no-op once the swap has renamed it away
	if err := os.RemoveAll(staged); err != nil {
		return "", err
	}
	if err := sys.CopyTree(src, staged); err != nil {
		return "", err
	}
	mapping := map[string]string{
		colours.accent: toHex(float64(accent[0]), float64(accent[1]), float64(accent[2])),
		// Hover/pressed states are drawn a step darker.
		scaled(oldAccent, 0.8): scaled(accent, 0.8),
	}

	err = filepath.WalkDir(staged, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if info, ierr := d.Info(); ierr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		switch {
		case textSuffixes[strings.ToLower(filepath.Ext(path))]:
			return sys.Rewrite(path, func(t string) (string, bool) {
				return recolourText(t, mapping)
			})
		case strings.EqualFold(filepath.Ext(path), ".png"):
			return recolourPNG(path, oldAccent, accent)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	// index.theme names the theme to GTK; it has to match the directory. It
	// carries the name on Name=, GtkTheme= and MetacityTheme=, so rewrite every
	// mention rather than just the first. A no-op when base == name.
	index := filepath.Join(staged, "index.theme")
	if sys.Exists(index) {
		if err := sys.Rewrite(index, func(t string) (string, bool) {
			out := strings.ReplaceAll(t, "Material-Black-"+base, "Material-Black-"+name)
			return out, out != t
		}); err != nil {
			return "", err
		}
	}

	if err := swap(staged, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func buildIcons(base, name string, accent RGB) (string, int, error) {
	src := sys.InHome(filepath.Join(".local/share/icons", "MB-"+base+"-Suru-GLOW"))
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return "", 0, fmt.Errorf("%s is not installed", src)
	}
	colours, err := readBase(base)
	if err != nil {
		return "", 0, err
	}
	mapping := map[string]string{
		colours.light: toHex(float64(accent[0]), float64(accent[1]), float64(accent[2])),
		colours.dark:  scaled(accent, 0.5),
	}

	dest := sys.InHome(filepath.Join(".local/share/icons", "MB-"+name+"-Suru-GLOW"))
	staged := staging(dest)
	defer os.RemoveAll(staged)
	if err := os.RemoveAll(staged); err != nil {
		return "", 0, err
	}
	if err := sys.CopyTree(src, staged); err != nil {
		return "", 0, err
	}

	count := 0
	err = filepath.WalkDir(staged, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.EqualFold(filepath.Ext(path), ".svg") {
			return nil
		}
		// Symlinked icons follow whatever they point at.
		if info, ierr := d.Info(); ierr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return sys.Rewrite(path, func(t string) (string, bool) {
			out, changed := recolourText(t, mapping)
			if changed {
				count++
			}
			return out, changed
		})
	})
	if err != nil {
		return "", 0, err
	}

	index := filepath.Join(staged, "index.theme")
	if sys.Exists(index) {
		if err := sys.Rewrite(index, func(t string) (string, bool) {
			out := sys.ReplaceFirst(nameRe, t, "Name=MB-"+name+"-Suru-GLOW")
			if !inheritsRe.MatchString(out) {
				out = sys.ReplaceFirstFunc(commentRe, out, func(m string) string {
					return m + "\nInherits=Papirus-Dark,Papirus,hicolor"
				})
			}
			return out, out != t
		}); err != nil {
			return "", 0, err
		}
	}

	if err := swap(staged, dest); err != nil {
		return "", 0, err
	}
	return dest, count, nil
}

// Recolor is the entry point behind `setup recolor`.
func Run(base, colour, name string) error {
	accent, err := parseHex(colour)
	if err != nil {
		return err
	}
	hex := toHex(float64(accent[0]), float64(accent[1]), float64(accent[2]))
	inPlace := base == name
	if inPlace {
		fmt.Printf("recolour %s -> %s\n", name, hex)
	} else {
		fmt.Printf("recolour %s -> %s as %s\n", base, hex, name)
	}

	gtk, err := buildGTK(base, name, accent)
	if err != nil {
		return err
	}
	fmt.Printf("  %s\n", gtk)

	icons, count, err := buildIcons(base, name, accent)
	if err != nil {
		return err
	}
	fmt.Printf("  %s (%d icons)\n", icons, count)

	// A re-derive is already pointed at by the palette that asked for it; the
	// advice only helps when a *new* variant has just appeared.
	if !inPlace {
		fmt.Printf("\nPoint a theme at it:\n  \"theme\": \"Material-Black-%s\",\n  \"icons\": \"MB-%s-Suru-GLOW\"\n",
			name, name)
	}
	return nil
}
