package recolor

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// The reference values throughout this file come from Python's colorsys and
// round(), which is what recolor.py used. Matching them is the contract: a
// rebuilt theme has to produce the same bytes the Python version did.

const eps = 1e-12

func TestRGBToHLS(t *testing.T) {
	cases := []struct {
		name    string
		r, g, b float64
		h, l, s float64
	}{
		{"purple", 0.5, 0.25, 0.75, 0.75, 0.5, 0.5},
		{"pure red", 1, 0, 0, 0, 0.5, 1},
		{"pure green", 0, 1, 0, 0.33333333333333331, 0.5, 1},
		{"pure blue", 0, 0, 1, 0.66666666666666663, 0.5, 1},
		// Achromatic inputs must report hue 0 and saturation 0 rather than
		// dividing by a zero range.
		{"grey", 0.2, 0.2, 0.2, 0, 0.20000000000000001, 0},
		{"black", 0, 0, 0, 0, 0, 0},
		{"white", 1, 1, 1, 0, 1, 0},
		{"yellowish", 0.9, 0.8, 0.1, 0.14583333333333334, 0.5, 0.80000000000000004},
		// l > 0.5 takes the other saturation branch.
		{"dark cyan", 0.1, 0.4, 0.35, 0.47222222222222215, 0.25, 0.60000000000000009},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, l, s := rgbToHLS(c.r, c.g, c.b)
			if math.Abs(h-c.h) > eps || math.Abs(l-c.l) > eps || math.Abs(s-c.s) > eps {
				t.Errorf("rgbToHLS(%v,%v,%v) = (%v,%v,%v), want (%v,%v,%v)",
					c.r, c.g, c.b, h, l, s, c.h, c.l, c.s)
			}
			if h < 0 || h >= 1 {
				t.Errorf("hue %v is outside [0,1) — Go's Mod keeps the sign where Python's %% does not", h)
			}
		})
	}
}

func TestHLSToRGB(t *testing.T) {
	cases := []struct {
		name    string
		h, l, s float64
		r, g, b float64
	}{
		{"red", 0.0, 0.5, 1.0, 1.0, 0.0, 0.0},
		{"purple", 0.75, 0.5, 0.5, 0.4999999999999998, 0.25, 0.75},
		{"light cyan", 0.5, 0.9, 0.2, 0.88, 0.92, 0.92},
		// Saturation is irrelevant at zero lightness.
		{"black", 0.1, 0.0, 1.0, 0.0, 0.0, 0.0},
		{"green-ish", 0.333, 0.25, 0.8, 0.0507999999999999, 0.45, 0.04999999999999999},
		{"pink", 0.9, 0.7, 0.6, 0.8799999999999999, 0.52, 0.7359999999999999},
		// s == 0 short-circuits to grey.
		{"grey", 0.4, 0.3, 0.0, 0.3, 0.3, 0.3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, g, b := hlsToRGB(c.h, c.l, c.s)
			if math.Abs(r-c.r) > eps || math.Abs(g-c.g) > eps || math.Abs(b-c.b) > eps {
				t.Errorf("hlsToRGB(%v,%v,%v) = (%v,%v,%v), want (%v,%v,%v)",
					c.h, c.l, c.s, r, g, b, c.r, c.g, c.b)
			}
		})
	}
}

// Every 8-bit colour must survive a trip through HLS and back, since that is
// exactly what recolouring a PNG pixel does.
func TestHLSRoundTrip(t *testing.T) {
	for r := 0; r < 256; r += 7 {
		for g := 0; g < 256; g += 11 {
			for b := 0; b < 256; b += 13 {
				h, l, s := rgbToHLS(float64(r)/255, float64(g)/255, float64(b)/255)
				nr, ng, nb := hlsToRGB(h, l, s)
				if clamp8(nr*255) != r || clamp8(ng*255) != g || clamp8(nb*255) != b {
					t.Fatalf("round trip of (%d,%d,%d) gave (%d,%d,%d)",
						r, g, b, clamp8(nr*255), clamp8(ng*255), clamp8(nb*255))
				}
			}
		}
	}
}

func TestHLSValueWrapsHue(t *testing.T) {
	// hlsToRGB feeds h±1/3 straight in, so out-of-range hues are routine.
	for _, h := range []float64{-1.25, -0.5, 0.25, 1.25, 2.5} {
		wrapped := math.Mod(h, 1)
		if wrapped < 0 {
			wrapped++
		}
		if got, want := hlsValue(0, 1, h), hlsValue(0, 1, wrapped); math.Abs(got-want) > eps {
			t.Errorf("hlsValue with hue %v = %v, want %v (same as wrapped %v)", h, got, want, wrapped)
		}
	}
}

func TestParseHex(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  RGB
		fails bool
	}{
		{name: "with hash", in: "#7fd8e8", want: RGB{127, 216, 232}},
		{name: "without hash", in: "7fd8e8", want: RGB{127, 216, 232}},
		{name: "uppercase", in: "#00E5CE", want: RGB{0, 229, 206}},
		{name: "surrounding space", in: "  #000000  ", want: RGB{0, 0, 0}},
		{name: "white", in: "#ffffff", want: RGB{255, 255, 255}},
		{name: "too short", in: "#fff", fails: true},
		{name: "too long", in: "#7fd8e8ff", fails: true},
		{name: "not hex", in: "#zzzzzz", fails: true},
		{name: "empty", in: "", fails: true},
		{name: "hash only", in: "#", fails: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseHex(c.in)
			if c.fails {
				if err == nil {
					t.Errorf("parseHex(%q) = %v, want an error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHex(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("parseHex(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// clamp8 must round half to even, like Python's round(). Rounding half up
// instead would shift a scattering of pixels by one and break byte-equality
// with the Python output.
func TestClamp8RoundsHalfToEven(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0.5, 0}, {1.5, 2}, {2.5, 2}, {3.5, 4},
		{0.4, 0}, {0.6, 1},
		{254.5, 254}, {255.5, 255}, // 256 clamps back down
		{-0.5, 0}, {-100, 0}, {300, 255}, {255, 255}, {0, 0},
	}
	for _, c := range cases {
		if got := clamp8(c.in); got != c.want {
			t.Errorf("clamp8(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestToHex(t *testing.T) {
	cases := []struct {
		r, g, b float64
		want    string
	}{
		{0, 229, 206, "#00e5ce"},
		{255, 255, 255, "#ffffff"},
		{0, 0, 0, "#000000"},
		{-5, 300, 127.5, "#00ff80"}, // clamped both ways, half-to-even in the middle
	}
	for _, c := range cases {
		if got := toHex(c.r, c.g, c.b); got != c.want {
			t.Errorf("toHex(%v,%v,%v) = %q, want %q", c.r, c.g, c.b, got, c.want)
		}
	}
}

func TestScaled(t *testing.T) {
	cases := []struct {
		name   string
		c      RGB
		factor float64
		want   string
	}{
		{"hover shade of teal", RGB{0, 229, 206}, 0.8, "#00b7a5"},
		{"dark gradient stop of teal", RGB{0, 229, 206}, 0.5, "#007267"},
		{"hover shade of ice blue", RGB{127, 216, 232}, 0.8, "#42c5dd"},
		{"dark gradient stop of ice blue", RGB{127, 216, 232}, 0.5, "#1b8598"},
		{"white halves to grey", RGB{255, 255, 255}, 0.5, "#808080"},
		{"black stays black", RGB{0, 0, 0}, 0.8, "#000000"},
		// Lightness is clamped to 1, so brightening cannot overflow.
		{"brightening clamps", RGB{200, 50, 50}, 1.5, "#e49393"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scaled(c.c, c.factor); got != c.want {
				t.Errorf("scaled(%v, %v) = %q, want %q", c.c, c.factor, got, c.want)
			}
		})
	}
}

func TestRecolourText(t *testing.T) {
	t.Run("case insensitive, both cases upstream uses", func(t *testing.T) {
		in := "a { color: #00E5CE; } b { color: #00e5ce; }"
		got, changed := recolourText(in, map[string]string{"#00e5ce": "#7fd8e8"})
		if !changed {
			t.Error("changed = false, want true")
		}
		want := "a { color: #7fd8e8; } b { color: #7fd8e8; }"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("no match leaves the text and reports no change", func(t *testing.T) {
		in := "a { color: #123456; }"
		got, changed := recolourText(in, map[string]string{"#00e5ce": "#7fd8e8"})
		if changed {
			t.Error("changed = true, want false")
		}
		if got != in {
			t.Errorf("got %q, want it unchanged", got)
		}
	})

	t.Run("both gradient stops in one pass", func(t *testing.T) {
		in := `stop-color:#00e5ce ... stop-color:#007267`
		got, changed := recolourText(in, map[string]string{
			"#00e5ce": "#7fd8e8",
			"#007267": "#1b8598",
		})
		if !changed {
			t.Error("changed = false, want true")
		}
		want := `stop-color:#7fd8e8 ... stop-color:#1b8598`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty mapping", func(t *testing.T) {
		if got, changed := recolourText("abc", nil); changed || got != "abc" {
			t.Errorf("got (%q, %v), want (\"abc\", false)", got, changed)
		}
	})
}

func TestAsNRGBAPassesThroughWithoutConverting(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 1})

	got := asNRGBA(src)
	if got != src {
		t.Error("asNRGBA copied an image that was already *image.NRGBA")
	}
}

// The bug this guards: image.Image.At() returns alpha-premultiplied values, and
// dividing back out quantises badly at low alpha. A colour at alpha 1 is the
// worst case — every channel collapses to a multiple of 255.
func TestAsNRGBAKeepsLowAlphaColoursIntact(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	want := color.NRGBA{R: 200, G: 100, B: 50, A: 1}
	src.SetNRGBA(0, 0, want)

	if got := asNRGBA(src).NRGBAAt(0, 0); got != want {
		t.Errorf("asNRGBA gave %+v, want %+v", got, want)
	}

	// Show what the premultiplying path would have done, so the test documents
	// why asNRGBA has to short-circuit rather than convert.
	viaAt := color.NRGBAModel.Convert(src.At(0, 0)).(color.NRGBA)
	if viaAt == want {
		t.Skip("this Go version no longer loses precision through At(); the guard is now moot")
	}
	t.Logf("via At() the same pixel becomes %+v — %d off on red", viaAt, int(want.R)-int(viaAt.R))
}

func TestAsNRGBAConvertsOtherImageTypes(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	// Opaque pixels are unaffected by premultiplication, so this conversion is
	// exact and safe.
	src.SetRGBA(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})

	got := asNRGBA(src)
	if _, ok := any(got).(*image.NRGBA); !ok {
		t.Fatal("asNRGBA did not return *image.NRGBA")
	}
	want := color.NRGBA{R: 200, G: 100, B: 50, A: 255}
	if px := got.NRGBAAt(0, 0); px != want {
		t.Errorf("converted pixel = %+v, want %+v", px, want)
	}
	if got.Bounds() != src.Bounds() {
		t.Errorf("bounds = %v, want %v", got.Bounds(), src.Bounds())
	}
}
