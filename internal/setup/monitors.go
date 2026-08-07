package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// Monitor layout is the one part of the Hyprland config that cannot live in the
// repo as written text. Every other file describes a preference, which is the
// same on every machine by definition — that is the point of the repo. A monitor
// block describes hardware, and the hardware is different on each machine.
//
// Hardcoding one machine's block and shipping it everywhere is what this
// replaces. It fails in two ways that are easy to miss, because neither says
// anything at the time:
//
//   - A refresh rate the panel does not have is not an error. Hyprland rejects
//     the mode and falls back to one it likes, so the layout comes up at
//     whatever it picked rather than what was written. A 120Hz line on a 60Hz
//     panel looks correct in the repo forever.
//   - A position is a claim about where a screen physically sits, which no
//     amount of reading the EDID can answer. Get it wrong and windows open on
//     the wrong screen and the pointer crosses edges that are not touching.
//
// So hydra asks the machine instead, the same way isLaptop does, and writes the
// answer to a generated file the config loads. What it asks for is deliberately
// narrow: the best mode each output actually advertises, at the position the
// output is already at. The position is not guessed — it is whatever the user
// has already arranged live, which is the only reliable source for it.

// monitorsTarget is the generated file hyprland.lua loads. It sits beside the
// other generated Hyprland config (hyprpaper.conf, colors.conf) rather than in
// the repo, because its contents are true of this machine only.
const monitorsTarget = ".config/hypr/monitors.lua"

// Monitor is the part of `hyprctl monitors` this needs. The field names are
// hyprctl's own.
type Monitor struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Width          int      `json:"width"`
	Height         int      `json:"height"`
	RefreshRate    float64  `json:"refreshRate"`
	X              int      `json:"x"`
	Y              int      `json:"y"`
	Scale          float64  `json:"scale"`
	Disabled       bool     `json:"disabled"`
	AvailableModes []string `json:"availableModes"`
}

// mode is a parsed entry from availableModes.
type mode struct {
	width, height int
	refresh       float64
}

// detectMonitors asks Hyprland what is actually connected.
//
// Disabled outputs are included: an output that is off because game mode turned
// it off is still part of this machine's layout, and dropping it would write a
// file that silently forgets a screen.
func detectMonitors() ([]Monitor, error) {
	out, ok := sys.Quiet("hyprctl", "-j", "monitors", "all")
	if !ok {
		return nil, fmt.Errorf("hyprctl did not answer; is Hyprland running?")
	}
	var mons []Monitor
	if err := json.Unmarshal([]byte(out), &mons); err != nil {
		return nil, fmt.Errorf("reading hyprctl monitors: %w", err)
	}
	if len(mons) == 0 {
		return nil, fmt.Errorf("hyprctl reported no monitors")
	}
	return mons, nil
}

// parseModes reads hyprctl's mode strings, e.g. "3840x2160@60.00Hz". Anything
// that does not parse is skipped rather than failing the run — one odd entry
// from a flaky EDID should not cost the user their whole layout.
func parseModes(raw []string) []mode {
	var out []mode
	for _, s := range raw {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "Hz"))
		res, hz, found := strings.Cut(s, "@")
		if !found {
			continue
		}
		w, h, found := strings.Cut(res, "x")
		if !found {
			continue
		}
		width, err := strconv.Atoi(strings.TrimSpace(w))
		if err != nil {
			continue
		}
		height, err := strconv.Atoi(strings.TrimSpace(h))
		if err != nil {
			continue
		}
		refresh, err := strconv.ParseFloat(strings.TrimSpace(hz), 64)
		if err != nil {
			continue
		}
		out = append(out, mode{width: width, height: height, refresh: refresh})
	}
	return out
}

// recordedMode is the mode written to the file: the one the output is running.
//
// It used to be bestMode, and that was the same mistake this whole file exists
// to avoid. The header above says a refresh rate the panel does not have is
// silently rejected and fallen back from — and "advertises" is not "can drive".
// hypr-cachy's three 4K panels each advertise 239.99Hz and cannot run three of
// them at once over the available bandwidth, so recording the best advertised
// mode wrote 239.99, Hyprland fell back, and the desk came up at 60Hz against
// a file that said 240. The running mode carries no such claim: it is being
// driven, right now, by this machine, on these cables.
//
// bestMode is kept for the note Monitors() prints, where "this panel also
// advertises something faster" is information rather than a decision.
//
// The rate is snapped to the advertised mode nearest the running one when there
// is a close match. hyprctl reports what the hardware is really doing —
// 59.997Hz for a panel whose mode list says 60.00 — and writing that back
// verbatim both reads badly and states a rate the output never offered. Snapping
// keeps the file to strings the panel actually advertises while still choosing
// among them by what is demonstrably running.
func recordedMode(m Monitor) mode {
	got := mode{width: m.Width, height: m.Height, refresh: m.RefreshRate}
	const tolerance = 0.5
	best, found := 0.0, false
	for _, c := range parseModes(m.AvailableModes) {
		if c.width != got.width || c.height != got.height {
			continue
		}
		if d := c.refresh - got.refresh; d < tolerance && d > -tolerance {
			if !found || c.refresh > best {
				best, found = c.refresh, true
			}
		}
	}
	if found {
		got.refresh = best
	}
	return got
}

// bestMode picks the highest refresh rate at the largest resolution the output
// advertises.
//
// Resolution wins over refresh rate: a 4K panel that can also do 1080p@144
// should not be driven at 1080p. Within the native resolution, faster is
// better.
//
// An output whose modes cannot be read at all falls back to what it is running
// now, which is at least a mode it demonstrably supports.
func bestMode(m Monitor) mode {
	modes := parseModes(m.AvailableModes)
	if len(modes) == 0 {
		return mode{width: m.Width, height: m.Height, refresh: m.RefreshRate}
	}
	best := modes[0]
	for _, c := range modes[1:] {
		switch {
		case c.width*c.height > best.width*best.height:
			best = c
		case c.width*c.height == best.width*best.height && c.refresh > best.refresh:
			best = c
		}
	}
	return best
}

// formatRefresh writes a rate the way a person would. Hyprland accepts a
// fractional rate and needs one for panels whose real rate is not an integer
// (239.99 rather than 240), but writing "60.00" for a plain 60Hz panel is
// noise in a file the user is meant to be able to read.
func formatRefresh(hz float64) string {
	if hz == float64(int(hz)) {
		return strconv.Itoa(int(hz))
	}
	return strconv.FormatFloat(hz, 'f', 2, 64)
}

// formatScale keeps whole scales whole, for the same reason.
func formatScale(s float64) string {
	if s == 0 {
		return "1"
	}
	if s == float64(int(s)) {
		return strconv.Itoa(int(s))
	}
	return strconv.FormatFloat(s, 'f', -1, 64)
}

// recordedScaleRe pulls `output = "DP-1"` … `scale = 2` back out of a file this
// package wrote. It reads hydra's own output in hydra's own format, so a loose
// regex is enough — anything it cannot parse simply yields no floor.
var recordedScaleRe = regexp.MustCompile(`output\s*=\s*"([^"]+)"[^\n]*?scale\s*=\s*([0-9.]+)`)

// recordedScales reads the scale each output was last recorded at.
//
// Scale is the one value in this file that is a *preference*, not a fact about
// the hardware: nothing in an EDID implies that a 4K panel wants scale 2, and
// no amount of asking Hyprland recovers the intent once something has reset it
// to 1. Everything else here — which outputs exist, what mode they run, where
// they sit — is observation, and observing it again is harmless. Re-observing
// scale is how a preference gets silently lost.
func recordedScales() map[string]float64 {
	raw, err := os.ReadFile(sys.InHome(monitorsTarget))
	if err != nil {
		return nil
	}
	out := map[string]float64{}
	for _, m := range recordedScaleRe.FindAllStringSubmatch(string(raw), -1) {
		if s, err := strconv.ParseFloat(m[2], 64); err == nil && s > 0 {
			out[m[1]] = s
		}
	}
	return out
}

// scaleFor is the scale to write for an output: the live one, unless a larger
// scale was recorded before and this is not a forced re-capture.
//
// Only a *drop* is held back. Raising a scale is something the user did on
// purpose in the compositor and plainly wants kept; a drop is equally often the
// user's doing and equally often the fallback this file was lost to once
// already. Holding the higher value costs a `--force` in the first case and
// saves the layout in the second, and it says which it did either way.
func scaleFor(m Monitor, recorded map[string]float64, force bool) (scale float64, held bool) {
	live := m.Scale
	if live == 0 {
		live = 1
	}
	if force {
		return live, false
	}
	if was, ok := recorded[m.Name]; ok && was > live {
		return was, true
	}
	return live, false
}

// renderMonitors writes the Lua the config loads.
//
// Outputs are ordered left to right, then top to bottom, so the generated file
// reads in the order the screens are actually sitting on the desk and a diff
// between two runs is about what changed rather than how hyprctl happened to
// enumerate them.
func renderMonitors(mons []Monitor, force bool) string {
	recorded := recordedScales()
	ordered := make([]Monitor, len(mons))
	copy(ordered, mons)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].X != ordered[j].X {
			return ordered[i].X < ordered[j].X
		}
		if ordered[i].Y != ordered[j].Y {
			return ordered[i].Y < ordered[j].Y
		}
		return ordered[i].Name < ordered[j].Name
	})

	var b strings.Builder
	b.WriteString(`-- Generated by hydra. Do not edit: `)
	b.WriteString("`hydra monitors` rewrites this file.\n")
	b.WriteString(`--
-- This machine's screens, as Hyprland reports them: the mode each output is
-- actually running, at the position it is currently arranged in. Rearrange or
-- replace a screen, run ` + "`hydra monitors`" + ` again, and the new layout lands here.
--
-- Scale is the exception. It is a preference rather than something to observe,
-- so a re-capture never lowers one: pass --force to accept a smaller scale.
`)

	for _, m := range ordered {
		best := recordedMode(m)
		scale, _ := scaleFor(m, recorded, force)
		b.WriteString(fmt.Sprintf("\n-- %s", m.Name))
		if d := strings.TrimSpace(m.Description); d != "" {
			b.WriteString(" — " + d)
		}
		b.WriteString("\n")
		// disabled = false is stated rather than left out. Hyprland merges a
		// monitor rule with the previous one for that output, and a `disabled =
		// true` set earlier survives a rule that simply does not mention it —
		// so an output the config had switched off stays off. Reloading this
		// file is then a reliable way back to the normal layout, whatever was
		// done to the screens since.
		//
		// It is written for every output, including ones that are off right
		// now. This file describes how the machine is normally arranged;
		// switching a screen off is a deviation applied on top of that, so
		// recording it would turn a temporary state into a permanent one.
		b.WriteString(fmt.Sprintf(
			"hl.monitor({ output = %q, mode = \"%dx%d@%s\", position = \"%dx%d\", scale = %s, disabled = false })\n",
			m.Name, best.width, best.height, formatRefresh(best.refresh),
			m.X, m.Y, formatScale(scale)))
	}
	return b.String()
}

// monitorsRecorded reports whether this machine has a layout on disk already.
func monitorsRecorded() bool { return sys.Exists(sys.InHome(monitorsTarget)) }

// layoutCheck reports an unrecorded layout instead of recording one.
//
// Nothing writes monitors.lua now except the user running `hydra monitors` —
// see the note in catalog.go for what auto-capture cost. A machine without one
// is not broken, it is on Hyprland's automatic arrangement, so this is a note
// and not a failure the sync stops for.
func layoutCheck() Check {
	return Check{
		Name: "monitor layout recorded (hyprland)",
		OK:   monitorsRecorded(),
		Fix: "arrange your screens, then run `hydra monitors` to record them — " +
			"until then Hyprland arranges them automatically",
	}
}

// Monitors is the `hydra monitors` command: capture the layout, then say what
// was captured, since the user cannot see the file being written.
func Monitors(force bool) error {
	mons, err := detectMonitors()
	if err != nil {
		return err
	}
	recorded := recordedScales()
	if err := sys.WriteFile(sys.InHome(monitorsTarget), renderMonitors(mons, force)); err != nil {
		return err
	}

	fmt.Println(titleStyle.Render("▸ " + monitorsTarget))
	ordered := make([]Monitor, len(mons))
	copy(ordered, mons)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].X < ordered[j].X })
	held := false
	for _, m := range ordered {
		got := recordedMode(m)
		scale, kept := scaleFor(m, recorded, force)
		line := fmt.Sprintf("  %-10s %dx%d@%s at %dx%d, scale %s",
			m.Name, got.width, got.height, formatRefresh(got.refresh), m.X, m.Y,
			formatScale(scale))
		// Say when the panel could go faster than it is being driven. This is
		// the note that used to be a decision — recording the faster mode is
		// what wrote a rate the machine could not drive.
		if b := bestMode(m); b.width == got.width && b.height == got.height &&
			b.refresh > got.refresh+0.5 {
			line += fmt.Sprintf("  (advertises %s)", formatRefresh(b.refresh))
		}
		if kept {
			held = true
			line += fmt.Sprintf("  (kept scale %s; live is %s)",
				formatScale(scale), formatScale(m.Scale))
		}
		fmt.Println(line)
	}
	if held {
		fmt.Println(dimStyle.Render("\n  a recorded scale is never lowered — " +
			"`hydra monitors --force` to accept the live one"))
	}
	fmt.Println(dimStyle.Render("\n  reload Hyprland to apply: hyprctl reload"))
	return nil
}
