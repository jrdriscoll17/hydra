package theme

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureStdout collects what fn prints, so the CLI commands can be tested on
// their actual output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(r)
		done <- string(raw)
	}()

	fn()

	w.Close()
	os.Stdout = original
	return <-done
}

// applyHome sets up a HOME where a full Apply can run: palettes, a Kvantum
// base, and no live-reload side effects.
func applyHome(t *testing.T, palettes map[string]string) string {
	t.Helper()
	home := newHome(t, palettes)
	noReload(t)
	useIconRoots(t, filepath.Join(t.TempDir(), "no-icons"))
	return home
}

func TestApply(t *testing.T) {
	home := applyHome(t, map[string]string{"fixture": fixturePalette})

	if err := Apply("fixture", true); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	t.Run("every renderer produced its file", func(t *testing.T) {
		for _, p := range []string{
			".config/quickshell/generated/Colors.qml",
			".config/quickshell/generated/icons.json",
			".config/kitty/theme.conf",
			".config/alacritty/colors.toml",
			".config/nvim/lua/theme.lua",
			".config/doom/theme.el",
			".config/hypr/hyprpaper.conf",
			".config/hypr/colors.conf",
			".config/btop/themes/fixture.theme",
			".config/gtk-3.0/settings.ini",
			".config/gtk-4.0/settings.ini",
			".gtkrc-2.0",
			".config/qt5ct/qt5ct.conf",
			".config/qt6ct/qt6ct.conf",
			".config/Kvantum/kvantum.kvconfig",
		} {
			if _, err := os.Stat(filepath.Join(home, p)); err != nil {
				t.Errorf("%s was not written: %v", p, err)
			}
		}
	})

	t.Run("the active theme is recorded", func(t *testing.T) {
		if got := strings.TrimSpace(readFile(t, filepath.Join(home, ".config/theme/current"))); got != "fixture" {
			t.Errorf("current = %q, want %q", got, "fixture")
		}
		if got := Current(); got != "fixture" {
			t.Errorf("Current() = %q, want %q", got, "fixture")
		}
	})
}

func TestApplyUnknownTheme(t *testing.T) {
	home := applyHome(t, map[string]string{"fixture": fixturePalette})

	if err := Apply("nope", true); err == nil {
		t.Fatal("Apply of a missing theme returned nil error")
	}
	// Nothing should have been written, and the recorded theme must not change.
	if _, err := os.Stat(filepath.Join(home, ".config/kitty/theme.conf")); !os.IsNotExist(err) {
		t.Error("Apply wrote output for a theme that does not exist")
	}
}

func TestApplyReportsWhichRendererFailed(t *testing.T) {
	applyHome(t, map[string]string{"fixture": fixturePalette})

	original := renderers
	renderers = []struct {
		name string
		fn   func(*Theme) error
	}{{"exploder", func(*Theme) error { return errors.New("deliberate test failure") }}}
	t.Cleanup(func() { renderers = original })

	err := Apply("fixture", true)
	if err == nil {
		t.Fatal("Apply returned nil despite a failing renderer")
	}
	if !strings.Contains(err.Error(), "exploder") {
		t.Errorf("error %q does not name the renderer that failed", err)
	}
}

func TestApplyQuiet(t *testing.T) {
	applyHome(t, map[string]string{"fixture": fixturePalette})

	quiet := captureStdout(t, func() {
		if err := Apply("fixture", true); err != nil {
			t.Errorf("Apply: %v", err)
		}
	})
	if quiet != "" {
		t.Errorf("quiet Apply printed %q, want nothing", quiet)
	}

	loud := captureStdout(t, func() {
		if err := Apply("fixture", false); err != nil {
			t.Errorf("Apply: %v", err)
		}
	})
	if !strings.Contains(loud, "Fixture") {
		t.Errorf("Apply printed %q, want the theme label", loud)
	}
}

// -- cmdList / cmdData -------------------------------------------------------

func TestCmdList(t *testing.T) {
	home := newHome(t, map[string]string{
		"fixture": fixturePalette,
		"other":   strings.Replace(fixturePalette, `"label": "Fixture"`, `"label": "Other"`, 1),
	})
	writeFile(t, filepath.Join(home, ".config/theme/current"), "other\n")

	out := captureStdout(t, func() {
		if err := cmdList(); err != nil {
			t.Errorf("cmdList: %v", err)
		}
	})

	for _, want := range []string{"fixture", "other", "Other", "a palette for tests"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The live theme is starred; the other is not.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "other") && !strings.HasPrefix(line, " *") {
			t.Errorf("the active theme is not marked:\n%s", out)
		}
		if strings.Contains(line, "fixture") && strings.HasPrefix(line, " *") {
			t.Errorf("an inactive theme is marked:\n%s", out)
		}
	}
}

// Quickshell's picker parses this, so its shape is a contract.
func TestCmdData(t *testing.T) {
	home := newHome(t, map[string]string{"fixture": fixturePalette})
	writeFile(t, filepath.Join(home, ".config/theme/current"), "fixture\n")

	out := captureStdout(t, func() {
		if err := cmdData(); err != nil {
			t.Errorf("cmdData: %v", err)
		}
	})

	var payload struct {
		Current string `json:"current"`
		Themes  []struct {
			Name      string            `json:"name"`
			Label     string            `json:"label"`
			Blurb     string            `json:"blurb"`
			Colors    map[string]string `json:"colors"`
			Wallpaper string            `json:"wallpaper"`
		} `json:"themes"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("cmdData did not emit valid JSON: %v\n%s", err, out)
	}

	if payload.Current != "fixture" {
		t.Errorf("current = %q, want %q", payload.Current, "fixture")
	}
	if len(payload.Themes) != 1 {
		t.Fatalf("themes = %d, want 1", len(payload.Themes))
	}
	th := payload.Themes[0]
	if th.Name != "fixture" || th.Label != "Fixture" || th.Blurb != "a palette for tests" {
		t.Errorf("theme entry = %+v", th)
	}
	if th.Colors["accent"] != "#111111" {
		t.Errorf("colors[accent] = %q, want #111111", th.Colors["accent"])
	}
	// The picker draws the wallpaper, so it needs a resolvable absolute path.
	if want := filepath.Join(home, ".config/hypr/wallpapers/test.jpg"); th.Wallpaper != want {
		t.Errorf("wallpaper = %q, want the absolute path %q", th.Wallpaper, want)
	}
}

// -- cmdIcons ----------------------------------------------------------------

func TestCmdIconsClearsTheCacheAndRebuilds(t *testing.T) {
	home := applyHome(t, map[string]string{"fixture": fixturePalette})
	iconFixture(t)
	writeFile(t, filepath.Join(home, ".config/theme/current"), "fixture\n")

	// A stale cache and marker, as after installing new apps.
	writeFile(t, filepath.Join(home, ".cache/theme", "icons-MB-Fixture-Suru-GLOW.json"),
		`{"stale":"/stale.svg"}`)
	writeFile(t, filepath.Join(home, ".config/quickshell/generated/icons.theme"),
		"MB-Fixture-Suru-GLOW\n")

	captureStdout(t, func() {
		if err := cmdIcons(); err != nil {
			t.Errorf("cmdIcons: %v", err)
		}
	})

	index := loadIndex(t, filepath.Join(home, ".config/quickshell/generated/icons.json"))
	if _, stale := index["stale"]; stale {
		t.Error("cmdIcons served the stale cache instead of rebuilding")
	}
	if index["firefox"] == "" {
		t.Error("the rebuilt index is missing icons")
	}
}

// -- Main --------------------------------------------------------------------

func TestMainDispatch(t *testing.T) {
	t.Run("no arguments lists", func(t *testing.T) {
		newHome(t, map[string]string{"fixture": fixturePalette})
		out := captureStdout(t, func() {
			if err := Main(nil); err != nil {
				t.Errorf("Main: %v", err)
			}
		})
		if !strings.Contains(out, "fixture") {
			t.Errorf("output = %q, want a theme listing", out)
		}
	})

	t.Run("current", func(t *testing.T) {
		home := newHome(t, map[string]string{"fixture": fixturePalette, "other": fixturePalette})
		writeFile(t, filepath.Join(home, ".config/theme/current"), "other\n")

		out := captureStdout(t, func() {
			if err := Main([]string{"current"}); err != nil {
				t.Errorf("Main: %v", err)
			}
		})
		if strings.TrimSpace(out) != "other" {
			t.Errorf("output = %q, want %q", out, "other")
		}
	})

	t.Run("help", func(t *testing.T) {
		newHome(t, map[string]string{"fixture": fixturePalette})
		for _, flag := range []string{"-h", "--help", "help"} {
			out := captureStdout(t, func() {
				if err := Main([]string{flag}); err != nil {
					t.Errorf("Main(%s): %v", flag, err)
				}
			})
			if !strings.Contains(out, "Global theme switcher") {
				t.Errorf("Main(%s) printed %q, want the usage text", flag, out)
			}
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		newHome(t, map[string]string{"fixture": fixturePalette})
		err := Main([]string{"frobnicate"})
		if err == nil {
			t.Fatal("Main of an unknown command returned nil error")
		}
		if !strings.Contains(err.Error(), "frobnicate") {
			t.Errorf("error %q does not name the command", err)
		}
	})

	t.Run("set requires a name", func(t *testing.T) {
		newHome(t, map[string]string{"fixture": fixturePalette})
		if err := Main([]string{"set"}); err == nil {
			t.Error("`set` with no argument returned nil error")
		}
	})

	t.Run("recolor requires three arguments", func(t *testing.T) {
		newHome(t, map[string]string{"fixture": fixturePalette})
		for _, argv := range [][]string{
			{"recolor"},
			{"recolor", "Base"},
			{"recolor", "Base", "#ffffff"},
			{"recolor", "Base", "#ffffff", "Name", "extra"},
		} {
			if err := Main(argv); err == nil {
				t.Errorf("Main(%v) returned nil error, want an arity complaint", argv)
			}
		}
	})
}

func TestMainSet(t *testing.T) {
	home := applyHome(t, map[string]string{"fixture": fixturePalette, "other": fixturePalette})

	captureStdout(t, func() {
		if err := Main([]string{"set", "other"}); err != nil {
			t.Errorf("Main set: %v", err)
		}
	})
	if got := strings.TrimSpace(readFile(t, filepath.Join(home, ".config/theme/current"))); got != "other" {
		t.Errorf("current = %q, want %q", got, "other")
	}
}

// A bare theme name is shorthand for `set`.
func TestMainBareThemeName(t *testing.T) {
	home := applyHome(t, map[string]string{"fixture": fixturePalette, "other": fixturePalette})

	captureStdout(t, func() {
		if err := Main([]string{"other"}); err != nil {
			t.Errorf("Main: %v", err)
		}
	})
	if got := strings.TrimSpace(readFile(t, filepath.Join(home, ".config/theme/current"))); got != "other" {
		t.Errorf("current = %q, want %q", got, "other")
	}
}

func TestMainNext(t *testing.T) {
	home := applyHome(t, map[string]string{
		"alpha": fixturePalette, "beta": fixturePalette, "gamma": fixturePalette,
	})

	// Names sort alpha, beta, gamma; `next` cycles and wraps.
	for _, want := range []string{"beta", "gamma", "alpha", "beta"} {
		captureStdout(t, func() {
			if err := Main([]string{"next"}); err != nil {
				t.Fatalf("Main next: %v", err)
			}
		})
		got := strings.TrimSpace(readFile(t, filepath.Join(home, ".config/theme/current")))
		if got != want {
			t.Fatalf("after next, current = %q, want %q", got, want)
		}
	}
}

func TestMainNextWithNoThemes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Main([]string{"next"}); err == nil {
		t.Error("`next` with no themes installed returned nil error")
	}
}

func TestMainApplyQuiet(t *testing.T) {
	home := applyHome(t, map[string]string{"fixture": fixturePalette})

	out := captureStdout(t, func() {
		if err := Main([]string{"apply", "--quiet"}); err != nil {
			t.Errorf("Main apply: %v", err)
		}
	})
	if out != "" {
		t.Errorf("`apply --quiet` printed %q, want nothing", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".config/kitty/theme.conf")); err != nil {
		t.Errorf("apply produced no output: %v", err)
	}
}

// -- setWallpaper ------------------------------------------------------------

// The line shape matters: hyprctl's own diagnostics contain a colon too, and
// matching them would make the switcher believe a wallpaper had been set.
func TestListactivePattern(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"one monitor", "DP-1: /home/jake/wall.jpg", []string{"/home/jake/wall.jpg"}},
		{
			"several monitors",
			"DP-1: /a.jpg\nDP-2: /b.jpg\nHDMI-A-1: /c.jpg",
			[]string{"/a.jpg", "/b.jpg", "/c.jpg"},
		},
		{"an hyprctl error is not a monitor line", "error: can't send: no such file", nil},
		{"empty", "", nil},
		{"a relative path is not matched", "DP-1: relative/path.jpg", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matches := listactiveRe.FindAllStringSubmatch(c.in, -1)
			var got []string
			for _, m := range matches {
				got = append(got, m[1])
			}
			if len(got) != len(c.want) {
				t.Fatalf("matched %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("match %d = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// hyprpaper not running is the normal case during `theme apply` at login: it
// reads the generated hyprpaper.conf when it starts, so there is nothing to
// retry and nothing to wait for.
func TestSetWallpaperReturnsFastWithoutHyprpaper(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no hyprctl on PATH

	done := make(chan bool, 1)
	go func() { done <- setWallpaper("/some/wall.jpg") }()

	select {
	case ok := <-done:
		if ok {
			t.Error("setWallpaper reported success with no hyprpaper running")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("setWallpaper blocked polling despite hyprpaper being absent")
	}
}
