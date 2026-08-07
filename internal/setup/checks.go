package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jrdriscoll17/hydra/internal/sys"
	"github.com/jrdriscoll17/hydra/internal/theme"
)

// Non-fatal system conditions worth reporting rather than silently failing on
// later.

// Check is a non-fatal system-level condition worth reporting rather than
// silently failing on later.
type Check struct {
	Name string
	OK   bool
	Fix  string
}

// themeAssetChecks verifies the GTK and icon themes the palettes point at.
// These are the one part of the setup nothing reproduces: the recolour derives
// Material-Black-<name> and MB-<name>-Suru-GLOW from an existing pair, the
// Colloid gtk4 themes are installed by hand, and none of it is packaged or in
// the repo. Missing assets do not error — you just get an unstyled desktop —
// so they are worth reporting explicitly.
func themeAssetChecks() []Check {
	var checks []Check
	for _, p := range palettes() {
		var gone []string
		for _, a := range []struct{ path, name string }{
			{sys.InHome(".themes/" + p.GTK.Theme), p.GTK.Theme},
			{sys.InHome(".themes/" + p.GTK.GTK4), p.GTK.GTK4},
			{sys.InHome(".local/share/icons/" + p.GTK.Icons), p.GTK.Icons},
		} {
			if a.name != "" && !sys.Exists(a.path) {
				gone = append(gone, a.name)
			}
		}

		checks = append(checks, Check{
			Name: "theme assets for " + p.Name,
			OK:   len(gone) == 0,
			Fix: fmt.Sprintf("missing: %s — re-run setup with the Theme switcher "+
				"component selected; it clones the upstream base and rebuilds these",
				strings.Join(gone, ", ")),
		})
	}
	return checks
}

// editorThemeCheck asks Neovim what colorscheme it is actually on, and compares
// that with the one the live palette names.
//
// Everything else a theme switch touches is settled by writing a file: the bar,
// the terminals, GTK, Qt and btop read what the render wrote, and a render that
// did not happen is caught by the stamp. Neovim is the exception. theme.lua
// only names a colorscheme; what the editor ends up on is whatever called
// `colorscheme` last, and a leftover spec, a colorscheme whose plugin never
// finished cloning, or a hand-edit all end the same way — every application on
// the machine follows the switch except the editor. There is no error, no
// warning, and no file that looks wrong, so the only way to know is to ask.
//
// Deliberately here rather than in `theme set`: a switch has no terminal to
// report to (the picker spawns it, and Quickshell detaches it), and this cannot
// fix anything anyway. `hydra status` is the command whose whole job is saying
// what is wrong with this machine, and its output is read.
//
// Anything short of a straight contradiction passes. No nvim, no palette, a
// probe that will not run or will not answer in time — none of those are
// evidence of a wrong theme, and a check that cries wolf when it cannot tell
// gets ignored, which is how the real one gets missed.
func editorThemeCheck() []Check {
	want := activePalette()
	if want == nil || want.Editors.Nvim == "" {
		return nil
	}
	got, ok := nvimColorscheme()
	if !ok {
		return nil
	}

	return []Check{{
		Name: "neovim is on the theme's colorscheme",
		OK:   got == want.Editors.Nvim,
		Fix: fmt.Sprintf("nvim comes up on %q, but the %s palette names %q — "+
			"something is setting the colorscheme after lua/plugins/colorscheme.lua "+
			"does. Check what `hydra status` lists as not in the repo under "+
			"~/.config/nvim, then re-run `nvim --headless \"+Lazy! sync\" +qa`",
			got, want.Name, want.Editors.Nvim),
	}}
}

// activePalette is the theme this machine is currently on, or nil if that
// cannot be established.
func activePalette() *theme.Theme {
	name := theme.Current()
	if name == "" {
		return nil
	}
	t, err := theme.Load(name)
	if err != nil {
		return nil
	}
	return t
}

// nvimColorscheme starts nvim with the user's real config and reports the
// colorscheme it settles on.
//
// The whole config has to load for the answer to mean anything — the failures
// this catches are all in what the config does — so this is a full startup, not
// a parse. It is cheap (well under a second here) but it is also entirely at
// the config's mercy, hence the deadline.
func nvimColorscheme() (string, bool) {
	if !sys.Have("nvim") {
		return "", false
	}
	// Written to stdout explicitly: in headless mode `print` goes to stderr
	// along with every message the config emits, and picking the answer back
	// out of that is guesswork.
	out, err := sys.CaptureWithin(15*time.Second, "nvim", "--headless",
		"-c", "lua io.stdout:write(tostring(vim.g.colors_name))", "-c", "qa")
	if err != nil {
		return "", false
	}
	// "nil" is what a config that set no colorscheme at all stringifies to.
	name := strings.TrimSpace(out)
	if name == "" || name == "nil" {
		return "", false
	}
	return name, true
}

func systemChecks(selected map[string]bool) []Check {
	var checks []Check

	if selected["theme"] {
		checks = append(checks, themeAssetChecks()...)
	}

	if selected["theme"] && selected["nvim"] {
		checks = append(checks, editorThemeCheck()...)
	}

	if selected["ddc"] {
		loaded := exec.Command("sh", "-c", "lsmod | grep -q '^i2c_dev'").Run() == nil
		checks = append(checks, Check{
			Name: "i2c-dev module loaded (ddcutil)",
			OK:   loaded,
			Fix:  "echo i2c-dev | sudo tee /etc/modules-load.d/i2c-dev.conf && sudo modprobe i2c-dev",
		})
		// Check the thing that actually matters — whether we can open an i2c
		// device — rather than group membership. udev rules can grant access
		// without the i2c group, so testing the group cries wolf on a machine
		// where ddcutil works perfectly well.
		checks = append(checks, Check{
			Name: "i2c devices readable (ddcutil without sudo)",
			OK:   i2cAccessible(),
			Fix:  "sudo usermod -aG i2c $USER   # then log out and back in",
		})
	}

	if selected["hyprland"] {
		checks = append(checks, layoutCheck())
	}

	if selected["core"] {
		// $SHELL reports whatever launched this process, which is not the login
		// shell when hydra is run from a script or another shell. passwd is the
		// authority.
		shell, _ := sys.Capture("sh", "-c", `getent passwd "$(id -un)" | cut -d: -f7`)
		checks = append(checks, Check{
			Name: "login shell is fish",
			OK:   strings.HasSuffix(shell, "/fish"),
			Fix:  "chsh -s /usr/bin/fish",
		})
	}

	checks = append(checks, Check{
		Name: "~/.local/bin on PATH",
		OK:   strings.Contains(os.Getenv("PATH"), sys.InHome(".local/bin")),
		Fix:  "add ~/.local/bin to fish_user_paths",
	})

	return checks
}

// i2cAccessible reports whether at least one /dev/i2c-* device can be opened
// read-write, which is what ddcutil needs. Opening the device performs no
// transaction, so this is a safe probe.
func i2cAccessible() bool {
	devices, err := filepath.Glob("/dev/i2c-*")
	if err != nil || len(devices) == 0 {
		return false
	}
	for _, dev := range devices {
		f, err := os.OpenFile(dev, os.O_RDWR, 0)
		if err == nil {
			f.Close()
			return true
		}
	}
	return false
}
