package setup

import (
	"strings"
	"testing"
)

func TestParseModes(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []mode
	}{
		{
			"hyprctl's own format",
			[]string{"3840x2160@60.00Hz", "1920x1080@144.00Hz"},
			[]mode{{3840, 2160, 60}, {1920, 1080, 144}},
		},
		{
			// The rate that made the hardcoded config wrong in the first place
			// is not an integer, so it has to survive the round trip.
			"a fractional rate",
			[]string{"3840x2160@239.99Hz"},
			[]mode{{3840, 2160, 239.99}},
		},
		{
			"whitespace and a missing Hz suffix",
			[]string{"  2560x1440@75  "},
			[]mode{{2560, 1440, 75}},
		},
		{
			// One unreadable entry should cost that entry and nothing else.
			"unparseable entries are skipped, not fatal",
			[]string{"garbage", "3840x2160@60.00Hz", "1920x@60Hz", "x1080@60Hz", "1920x1080@Hz"},
			[]mode{{3840, 2160, 60}},
		},
		{"nothing at all", nil, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseModes(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("parseModes(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("parseModes(%q)[%d] = %v, want %v", c.in, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestBestMode(t *testing.T) {
	cases := []struct {
		name string
		mon  Monitor
		want mode
	}{
		{
			// The bug this exists to prevent: the config asked for 120 on a
			// panel whose top mode is 60, and Hyprland quietly used something
			// else. The best mode has to come from what the panel advertises.
			"the fastest rate at the native resolution",
			Monitor{AvailableModes: []string{
				"3840x2160@30.00Hz", "3840x2160@60.00Hz", "2560x1440@144.00Hz",
			}},
			mode{3840, 2160, 60},
		},
		{
			// Resolution outranks refresh rate: a faster small mode is not an
			// upgrade on a 4K panel.
			"resolution wins over refresh rate",
			Monitor{AvailableModes: []string{"1920x1080@240.00Hz", "3840x2160@60.00Hz"}},
			mode{3840, 2160, 60},
		},
		{
			"the fastest rate when several share the top resolution",
			Monitor{AvailableModes: []string{
				"3840x2160@60.00Hz", "3840x2160@239.99Hz", "3840x2160@120.00Hz",
			}},
			mode{3840, 2160, 239.99},
		},
		{
			// An output that cannot describe itself still has a mode it is
			// demonstrably running.
			"falls back to the current mode when no modes are listed",
			Monitor{Width: 2880, Height: 1800, RefreshRate: 90, AvailableModes: nil},
			mode{2880, 1800, 90},
		},
		{
			"falls back when every listed mode is unreadable",
			Monitor{Width: 1920, Height: 1080, RefreshRate: 60,
				AvailableModes: []string{"nonsense", "also nonsense"}},
			mode{1920, 1080, 60},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bestMode(c.mon); got != c.want {
				t.Errorf("bestMode() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFormatRefresh(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{60, "60"},
		{120, "120"},
		{239.99, "239.99"},
		{59.997, "60.00"},
	}
	for _, c := range cases {
		if got := formatRefresh(c.in); got != c.want {
			t.Errorf("formatRefresh(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatScale(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{2, "2"},
		{1, "1"},
		{1.5, "1.5"},
		// An output hyprctl reported no scale for is not an output at scale 0.
		{0, "1"},
	}
	for _, c := range cases {
		if got := formatScale(c.in); got != c.want {
			t.Errorf("formatScale(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The work desktop that prompted this: three 4K panels, none faster than 60Hz,
// with the odd one out physically in the middle.
func workDesktop() []Monitor {
	sixty := []string{"3840x2160@60.00Hz", "3840x2160@29.98Hz", "1920x1080@60.00Hz"}
	return []Monitor{
		{Name: "DP-1", Description: "AOC U2790B", Width: 3840, Height: 2160,
			RefreshRate: 59.997, X: 3840, Y: 0, Scale: 2, AvailableModes: sixty},
		{Name: "DP-2", Description: "AOC U2790B", Width: 3840, Height: 2160,
			RefreshRate: 59.997, X: 0, Y: 0, Scale: 2, AvailableModes: sixty},
		{Name: "DP-3", Description: "AOC U27B3A", Width: 3840, Height: 2160,
			RefreshRate: 60, X: 1920, Y: 0, Scale: 2, AvailableModes: sixty},
	}
}

func TestRenderMonitorsOrdersLeftToRight(t *testing.T) {
	got := renderMonitors(workDesktop())

	var outputs []string
	for line := range strings.SplitSeq(got, "\n") {
		if strings.HasPrefix(line, "hl.monitor(") {
			outputs = append(outputs, line)
		}
	}
	if len(outputs) != 3 {
		t.Fatalf("got %d monitor lines, want 3:\n%s", len(outputs), got)
	}

	want := []string{
		`hl.monitor({ output = "DP-2", mode = "3840x2160@60", position = "0x0", scale = 2, disabled = false })`,
		`hl.monitor({ output = "DP-3", mode = "3840x2160@60", position = "1920x0", scale = 2, disabled = false })`,
		`hl.monitor({ output = "DP-1", mode = "3840x2160@60", position = "3840x0", scale = 2, disabled = false })`,
	}
	for i := range want {
		if outputs[i] != want[i] {
			t.Errorf("line %d:\n got %s\nwant %s", i, outputs[i], want[i])
		}
	}
}

// hyprctl enumerates by output id, which is not the order the screens sit in.
// The generated file has to read the same either way, or every run produces a
// diff that says nothing.
func TestRenderMonitorsIsStableAcrossEnumerationOrder(t *testing.T) {
	forward := workDesktop()
	reversed := []Monitor{forward[2], forward[0], forward[1]}

	if a, b := renderMonitors(forward), renderMonitors(reversed); a != b {
		t.Errorf("render depends on input order:\n--- a\n%s\n--- b\n%s", a, b)
	}
}

func TestRenderMonitorsNegativePositions(t *testing.T) {
	// A screen to the left of the primary sits at a negative x, and sorts first.
	mons := []Monitor{
		{Name: "DP-2", X: 0, Scale: 2, AvailableModes: []string{"3840x2160@60.00Hz"}},
		{Name: "HDMI-A-1", X: -1920, Scale: 2, AvailableModes: []string{"3840x2160@60.00Hz"}},
	}
	got := renderMonitors(mons)

	hdmi := strings.Index(got, `output = "HDMI-A-1"`)
	dp := strings.Index(got, `output = "DP-2"`)
	if hdmi == -1 || dp == -1 {
		t.Fatalf("both outputs should be rendered:\n%s", got)
	}
	if hdmi > dp {
		t.Errorf("HDMI-A-1 is at x=-1920 and should sort first:\n%s", got)
	}
	if !strings.Contains(got, `position = "-1920x0"`) {
		t.Errorf("negative position not preserved:\n%s", got)
	}
}

// An output that is off right now is still part of the normal layout — it is
// off because something switched it off, which is a deviation from this file
// rather than a fact about the machine. Capturing during game mode must not
// bake "this screen is off" into the layout for good.
func TestRenderMonitorsRecordsDisabledOutputsAsEnabled(t *testing.T) {
	mons := []Monitor{
		{Name: "DP-1", X: 0, Scale: 2, Disabled: true,
			AvailableModes: []string{"3840x2160@60.00Hz"}},
	}
	got := renderMonitors(mons)
	if !strings.Contains(got, `output = "DP-1"`) {
		t.Fatalf("disabled output was dropped:\n%s", got)
	}
	if !strings.Contains(got, "disabled = false") {
		t.Errorf("a screen that is off now was recorded as off for good:\n%s", got)
	}
	if strings.Contains(got, "disabled = true") {
		t.Errorf("temporary state leaked into the layout:\n%s", got)
	}
}

// Every line has to carry the flag, not just the ones that were off: it is
// what makes reloading the file a reliable way back to the normal layout.
func TestRenderMonitorsAlwaysStatesDisabledFalse(t *testing.T) {
	got := renderMonitors(workDesktop())
	if n := strings.Count(got, "disabled = false"); n != 3 {
		t.Errorf("got %d `disabled = false`, want 3:\n%s", n, got)
	}
}

func TestRenderMonitorsCarriesDescription(t *testing.T) {
	got := renderMonitors(workDesktop())
	if !strings.Contains(got, "AOC U27B3A") {
		t.Errorf("monitor description missing, which is what identifies the panel:\n%s", got)
	}
}

// The header has to say the file is generated, since it lands in a directory
// the user otherwise edits by hand.
func TestRenderMonitorsIsMarkedGenerated(t *testing.T) {
	got := renderMonitors(workDesktop())
	if !strings.HasPrefix(got, "-- Generated by hydra") {
		t.Errorf("generated file is not marked as one:\n%s", got)
	}
	if !strings.Contains(got, "hydra monitors") {
		t.Error("the header should name the command that rewrites the file")
	}
}
