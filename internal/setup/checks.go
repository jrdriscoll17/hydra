package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
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

func systemChecks(selected map[string]bool) []Check {
	var checks []Check

	if selected["theme"] {
		checks = append(checks, themeAssetChecks()...)
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
