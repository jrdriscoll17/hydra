package setup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestOwnedBy(t *testing.T) {
	roots := []string{".config/nvim", ".tmux.conf", ".config/git"}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"exact match on a directory", ".config/nvim", true},
		{"exact match on a file", ".tmux.conf", true},
		{"a file inside an owned directory", ".config/nvim/init.lua", true},
		{"nested deeply", ".config/nvim/lua/plugins/colorscheme.lua", true},
		{"unowned path", ".config/fish/config.fish", false},
		// The separator matters: without it ".config/nvim-backup" would be
		// claimed by the nvim component.
		{"a sibling with the owned path as a prefix", ".config/nvim-backup/x", false},
		{"a sibling file with the same prefix", ".tmux.conf.bak", false},
		{"a parent of an owned path", ".config", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ownedBy(c.path, roots); got != c.want {
				t.Errorf("ownedBy(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestOwnedByNoRoots(t *testing.T) {
	if ownedBy(".config/nvim", nil) {
		t.Error("ownedBy with no roots returned true")
	}
}

// chezmoi resolves a relative argument against the working directory, while
// `chezmoi status` hands back paths relative to the target root. Passing one
// straight back made `hydra sync` work from $HOME and fail everywhere else, on
// "not managed" — so every path handed to chezmoi is anchored first.
func TestTargetPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := targetPath(".config/hypr/hyprland.lua"),
		filepath.Join(home, ".config/hypr/hyprland.lua"); got != want {
		t.Errorf("targetPath = %q, want %q", got, want)
	}

	// Already absolute: chezmoi needs no help, and rooting it again would
	// produce nonsense like $HOME/home/jake/...
	abs := filepath.Join(home, ".gtkrc-2.0")
	if got := targetPath(abs); got != abs {
		t.Errorf("targetPath(%q) = %q, want it unchanged", abs, got)
	}
}

func TestParseStatus(t *testing.T) {
	roots := []string{".config/nvim", ".config/fish", ".tmux.conf"}

	// The real two-column format: the first column is the source state, the
	// second the target state.
	out := `
 M .config/nvim/init.lua
A  .config/fish/config.fish
MM .tmux.conf
 A .config/nvim/lua/theme.lua
 D .config/nvim/deleted.lua
 M .config/kitty/kitty.conf
`
	got := parseStatus(out, roots)

	want := map[string]FileState{
		".config/nvim/init.lua":      StateConflict,
		".config/fish/config.fish":   StateNew,
		".tmux.conf":                 StateConflict,
		".config/nvim/lua/theme.lua": StateNew,
	}
	if len(got) != len(want) {
		t.Errorf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for path, state := range want {
		if got[path] != state {
			t.Errorf("%s = %v, want %v", path, got[path], state)
		}
	}

	t.Run("unowned paths are ignored", func(t *testing.T) {
		if _, ok := got[".config/kitty/kitty.conf"]; ok {
			t.Error("a path outside the selected components was reported")
		}
	})

	// Deletions are chezmoi's business, not this tool's — hydra only creates
	// and overwrites.
	t.Run("deletions are not reported", func(t *testing.T) {
		if _, ok := got[".config/nvim/deleted.lua"]; ok {
			t.Error("a deletion was reported as drift")
		}
	})
}

// M wins over A when both columns are set, because the target existing and
// differing is what needs a decision.
func TestParseStatusModifiedWinsOverAdded(t *testing.T) {
	got := parseStatus("AM .config/nvim/init.lua", []string{".config/nvim"})
	if got[".config/nvim/init.lua"] != StateConflict {
		t.Errorf("AM = %v, want StateConflict", got[".config/nvim/init.lua"])
	}
}

func TestParseStatusIgnoresJunk(t *testing.T) {
	cases := []string{"", "\n\n", "ab", " M", "x"}
	for _, in := range cases {
		if got := parseStatus(in, []string{".config"}); len(got) != 0 {
			t.Errorf("parseStatus(%q) = %v, want empty", in, got)
		}
	}
}

func TestParseStatusHandlesPathsWithSpaces(t *testing.T) {
	got := parseStatus(` M .config/nvim/a file.lua`, []string{".config/nvim"})
	if got[".config/nvim/a file.lua"] != StateConflict {
		t.Errorf("got %v, want the spaced path reported", got)
	}
}

// -- host detection ----------------------------------------------------------

func TestIsLaptop(t *testing.T) {
	t.Run("battery present", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, filepath.Join(dir, "BAT0"))
		mkdir(t, filepath.Join(dir, "AC"))
		usePowerSupply(t, dir)

		if !isLaptop() {
			t.Error("isLaptop = false with a BAT0 present")
		}
	})

	t.Run("mains only", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, filepath.Join(dir, "AC"))
		mkdir(t, filepath.Join(dir, "hidpp_battery_0")) // a wireless mouse, not a laptop
		usePowerSupply(t, dir)

		if isLaptop() {
			t.Error("isLaptop = true with no BAT* entry")
		}
	})

	t.Run("no power supply directory at all", func(t *testing.T) {
		usePowerSupply(t, filepath.Join(t.TempDir(), "absent"))
		if isLaptop() {
			t.Error("isLaptop = true with no /sys/class/power_supply")
		}
	})

	t.Run("a second battery still counts", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, filepath.Join(dir, "BAT1"))
		usePowerSupply(t, dir)
		if !isLaptop() {
			t.Error("isLaptop = false with BAT1 present")
		}
	})
}

func TestArchLabel(t *testing.T) {
	dir := t.TempDir()
	usePowerSupply(t, dir)
	if got := archLabel(); got != "desktop" {
		t.Errorf("archLabel = %q, want %q", got, "desktop")
	}

	mkdir(t, filepath.Join(dir, "BAT0"))
	if got := archLabel(); got != "laptop" {
		t.Errorf("archLabel = %q, want %q", got, "laptop")
	}
}

// -- helpers -----------------------------------------------------------------

func usePowerSupply(t *testing.T, dir string) {
	t.Helper()
	original := powerSupplyDir
	powerSupplyDir = dir
	t.Cleanup(func() { powerSupplyDir = original })
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

func keysOf(components []Component) []string {
	var out []string
	for _, c := range components {
		out = append(out, c.Key)
	}
	slices.Sort(out)
	return out
}
