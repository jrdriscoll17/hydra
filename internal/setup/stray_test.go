package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unmanagedStub makes `chezmoi unmanaged <dir>` answer with the lines the map
// gives for that directory, and fail for any other subcommand — nothing here
// should be reaching for a different one.
func unmanagedStub(t *testing.T, byDir map[string][]string) {
	t.Helper()

	var cases strings.Builder
	for dir, paths := range byDir {
		fmt.Fprintf(&cases, "  %s)\n    cat <<'EOF'\n%s\nEOF\n    ;;\n",
			dir, strings.Join(paths, "\n"))
	}

	script := "#!/bin/sh\n" +
		"[ \"$1\" = unmanaged ] || exit 1\n" +
		"case \"$2\" in\n" + cases.String() + "  *) : ;;\nesac\n"

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chezmoi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The leftover that started all this: a plugin spec the repo dropped, still
// sitting in the directory lazy.nvim imports wholesale.
func TestStraysReportsAFileTheRepoNoLongerHas(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdir(t, filepath.Join(home, ".config/nvim"))
	unmanagedStub(t, map[string][]string{
		filepath.Join(home, ".config/nvim"): {".config/nvim/lua/plugins/onedark.lua"},
	})

	got := strays([]Component{{Key: "nvim", Exclusive: []string{".config/nvim"}}})
	if !slices.Equal(got, []string{".config/nvim/lua/plugins/onedark.lua"}) {
		t.Errorf("strays = %v, want the leftover plugin spec", got)
	}
}

// A component with no exclusive directory is not scanned at all: most of them
// sit in directories that legitimately hold files from elsewhere — fish's
// conf.d picks up whatever oh-my-fish and rvm put there — and reporting those
// as leftovers would be wrong every time.
func TestStraysIgnoresComponentsWithNoExclusiveDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdir(t, filepath.Join(home, ".config/fish"))
	unmanagedStub(t, map[string][]string{
		filepath.Join(home, ".config/fish"): {".config/fish/conf.d/omf.fish"},
	})

	if got := strays([]Component{{Key: "core", Paths: []string{".config/fish"}}}); len(got) != 0 {
		t.Errorf("strays = %v, want none for a component that owns no directory outright", got)
	}
}

// hydra's own backups live in the directories it scans. Reporting them would
// mean nagging about its own tidying forever, and moving each backup aside to a
// backup of itself on every run.
func TestStraysSkipsItsOwnBackups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdir(t, filepath.Join(home, ".config/hypr"))
	unmanagedStub(t, map[string][]string{
		filepath.Join(home, ".config/hypr"): {
			".config/hypr/hyprland.conf",
			".config/hypr/wallpapers.before-setup",
			".config/hypr/hyprland.conf.before-setup.2",
		},
	})

	got := strays([]Component{{Key: "hyprland", Exclusive: []string{".config/hypr"}}})
	if !slices.Equal(got, []string{".config/hypr/hyprland.conf"}) {
		t.Errorf("strays = %v, want only the leftover config", got)
	}
}

// Two components naming the same directory must not report it twice.
func TestStraysDeduplicates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdir(t, filepath.Join(home, ".config/nvim"))
	unmanagedStub(t, map[string][]string{
		filepath.Join(home, ".config/nvim"): {".config/nvim/after/plugin/old.lua"},
	})

	got := strays([]Component{
		{Key: "nvim", Exclusive: []string{".config/nvim"}},
		{Key: "other", Exclusive: []string{".config/nvim"}},
	})
	if len(got) != 1 {
		t.Errorf("strays = %v, want one entry for a directory named twice", got)
	}
}

// A directory this machine does not have is not a directory full of leftovers.
func TestStraysSkipsAbsentDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	unmanagedStub(t, map[string][]string{
		filepath.Join(home, ".config/doom"): {".config/doom/stale.el"},
	})

	if got := strays([]Component{{Key: "emacs", Exclusive: []string{".config/doom"}}}); len(got) != 0 {
		t.Errorf("strays = %v, want none when the directory is not there", got)
	}
}

// chezmoi failing to answer is not evidence that nothing is stray, and this is
// a report: a wrong one is worse than none.
func TestStraysStaysQuietWhenChezmoiFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdir(t, filepath.Join(home, ".config/nvim"))

	stubs := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubs, "chezmoi"),
		[]byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := strays([]Component{{Key: "nvim", Exclusive: []string{".config/nvim"}}}); len(got) != 0 {
		t.Errorf("strays = %v, want none when chezmoi cannot answer", got)
	}
}

func TestIsBackup(t *testing.T) {
	cases := map[string]bool{
		".config/hypr/wallpapers.before-setup":       true,
		".config/hypr/hyprland.conf.before-setup.2":  true,
		".config/nvim/lua/plugins/onedark.lua":       false,
		".config/nvim/before-setup.lua":              false,
		".config/quickshell/Bar.qml":                 false,
		".config/nvim/lua/plugins/x.before-setup.la": false,
	}
	for path, want := range cases {
		if got := isBackup(path); got != want {
			t.Errorf("isBackup(%q) = %v, want %v", path, got, want)
		}
	}
}

// Clearing a leftover has to actually empty the directory of it — a copy that
// left the original in place would leave it being loaded, which is the whole
// failure.
func TestClearMovesTheLeftoverOutOfTheDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".config/nvim/lua/plugins/onedark.lua"), "return {}\n")

	dst, err := clear(".config/nvim/lua/plugins/onedark.lua")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if exists(filepath.Join(home, ".config/nvim/lua/plugins/onedark.lua")) {
		t.Error("the leftover is still where lazy.nvim would import it")
	}
	if got := readFile(t, dst); got != "return {}\n" {
		t.Errorf("moved-aside contents = %q, want the file preserved", got)
	}
}

// A path that is already gone is not an error: the user may have removed it
// between the report and the answer.
func TestClearIgnoresAnAbsentPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := clear(".config/nvim/lua/plugins/gone.lua"); err != nil {
		t.Errorf("clear on an absent path returned %v", err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
