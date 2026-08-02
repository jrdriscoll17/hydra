package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkNamed finds a check by a fragment of its name.
func checkNamed(checks []Check, fragment string) (Check, bool) {
	for _, c := range checks {
		if strings.Contains(c.Name, fragment) {
			return c, true
		}
	}
	return Check{}, false
}

func TestThemeAssetChecks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "Colloid-Dark", "#7fd8e8")

	t.Run("reports missing assets", func(t *testing.T) {
		checks := themeAssetChecks()
		if len(checks) != 1 {
			t.Fatalf("got %d checks, want one per palette", len(checks))
		}
		if checks[0].OK {
			t.Error("check passed with no assets on disk")
		}
		// The fix text names what is actually missing, so it has to list all
		// three.
		for _, want := range []string{"Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "Colloid-Dark"} {
			if !strings.Contains(checks[0].Fix, want) {
				t.Errorf("fix text %q does not mention %q", checks[0].Fix, want)
			}
		}
	})

	t.Run("passes once everything is installed", func(t *testing.T) {
		mkdir(t, filepath.Join(home, ".themes/Material-Black-IceBlue"))
		mkdir(t, filepath.Join(home, ".themes/Colloid-Dark"))
		mkdir(t, filepath.Join(home, ".local/share/icons/MB-IceBlue-Suru-GLOW"))

		checks := themeAssetChecks()
		if !checks[0].OK {
			t.Errorf("check failed with everything present: %s", checks[0].Fix)
		}
	})
}

// A palette that names no gtk4 theme must not be reported as missing one.
func TestThemeAssetChecksIgnoresAnEmptyGTK4Name(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")
	mkdir(t, filepath.Join(home, ".themes/Material-Black-IceBlue"))
	mkdir(t, filepath.Join(home, ".local/share/icons/MB-IceBlue-Suru-GLOW"))

	checks := themeAssetChecks()
	if !checks[0].OK {
		t.Errorf("check failed over an unset gtk4 theme: %s", checks[0].Fix)
	}
}

func TestThemeAssetChecksOnePerPalette(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "a", "Material-Black-A", "MB-A-Suru-GLOW", "", "#111111")
	palette(t, home, "b", "Material-Black-B", "MB-B-Suru-GLOW", "", "#222222")

	if got := len(themeAssetChecks()); got != 2 {
		t.Errorf("got %d checks, want 2", got)
	}
}

func TestSystemChecksAreGatedOnSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("nothing selected", func(t *testing.T) {
		checks := systemChecks(map[string]bool{})
		if _, found := checkNamed(checks, "i2c"); found {
			t.Error("an i2c check appeared without the ddc component")
		}
		if _, found := checkNamed(checks, "login shell"); found {
			t.Error("the shell check appeared without the core component")
		}
		if _, found := checkNamed(checks, "theme assets"); found {
			t.Error("theme asset checks appeared without the theme component")
		}
	})

	t.Run("ddc selected", func(t *testing.T) {
		checks := systemChecks(map[string]bool{"ddc": true})
		if _, found := checkNamed(checks, "i2c-dev module"); !found {
			t.Error("no i2c-dev module check with ddc selected")
		}
		if _, found := checkNamed(checks, "i2c devices readable"); !found {
			t.Error("no i2c access check with ddc selected")
		}
	})

	t.Run("core selected", func(t *testing.T) {
		checks := systemChecks(map[string]bool{"core": true})
		if _, found := checkNamed(checks, "login shell is fish"); !found {
			t.Error("no login shell check with core selected")
		}
	})

	t.Run("theme selected", func(t *testing.T) {
		palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")
		checks := systemChecks(map[string]bool{"theme": true})
		if _, found := checkNamed(checks, "theme assets for ice"); !found {
			t.Error("no theme asset check with the theme component selected")
		}
	})
}

// The PATH check runs regardless of selection, because `theme` lives there and
// Quickshell execs it by name.
func TestPathCheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("absent", func(t *testing.T) {
		t.Setenv("PATH", "/usr/bin:/bin")
		check, found := checkNamed(systemChecks(nil), "~/.local/bin on PATH")
		if !found {
			t.Fatal("no PATH check")
		}
		if check.OK {
			t.Error("PATH check passed with ~/.local/bin absent")
		}
	})

	t.Run("present", func(t *testing.T) {
		t.Setenv("PATH", "/usr/bin:"+filepath.Join(home, ".local/bin")+":/bin")
		check, _ := checkNamed(systemChecks(nil), "~/.local/bin on PATH")
		if !check.OK {
			t.Error("PATH check failed with ~/.local/bin present")
		}
	})
}

func TestEveryCheckCarriesAFix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	palette(t, home, "ice", "Material-Black-IceBlue", "MB-IceBlue-Suru-GLOW", "", "#7fd8e8")

	all := map[string]bool{"core": true, "ddc": true, "theme": true}
	for _, c := range systemChecks(all) {
		if c.Name == "" {
			t.Error("a check has no name")
		}
		// A failing check prints its Fix as "still to do by hand", so an empty
		// one leaves the user stuck.
		if c.Fix == "" {
			t.Errorf("check %q has no fix text", c.Name)
		}
	}
}

// i2cAccessible probes whether a device can actually be opened, rather than
// testing group membership — udev can grant access without the i2c group, and
// checking the group cried wolf on a machine where ddcutil works.
func TestI2CAccessible(t *testing.T) {
	got := i2cAccessible()

	devices, err := filepath.Glob("/dev/i2c-*")
	if err != nil || len(devices) == 0 {
		if got {
			t.Error("i2cAccessible = true with no /dev/i2c-* devices at all")
		}
		return
	}

	// With devices present the answer depends on this machine's permissions;
	// assert only that it agrees with an independent open attempt.
	want := false
	for _, dev := range devices {
		if f, err := os.OpenFile(dev, os.O_RDWR, 0); err == nil {
			f.Close()
			want = true
			break
		}
	}
	if got != want {
		t.Errorf("i2cAccessible = %v, but opening a device directly gave %v", got, want)
	}
}
