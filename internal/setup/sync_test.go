package setup

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// quiet swallows stdout while fn runs; several of these functions report
// progress as they go.
func quiet(t *testing.T, fn func()) string {
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

// `go install` produces exactly one executable, but Quickshell execs `theme`
// by name — so this symlink is the only thing making the switcher reachable on
// a freshly bootstrapped machine.
func TestLinkThemeName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	quiet(t, func() {
		if err := linkThemeName(); err != nil {
			t.Errorf("linkThemeName: %v", err)
		}
	})

	link := filepath.Join(home, ".local/bin/theme")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("`theme` is not a symlink: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip("cannot determine this executable")
	}
	if target != self {
		t.Errorf("theme -> %q, want this binary %q", target, self)
	}
}

func TestLinkThemeNameReplacesAnExistingLink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	link := filepath.Join(home, ".local/bin/theme")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	// An upgrade leaves a link to the previous location behind.
	if err := os.Symlink(filepath.Join(home, "old-hydra"), link); err != nil {
		t.Fatal(err)
	}

	quiet(t, func() {
		if err := linkThemeName(); err != nil {
			t.Errorf("linkThemeName: %v", err)
		}
	})

	self, _ := os.Executable()
	if target, _ := os.Readlink(link); target != self {
		t.Errorf("stale link not replaced: -> %q, want %q", target, self)
	}
}

// A real file at that path (not a link) must also give way, or the switcher
// stays unreachable.
func TestLinkThemeNameReplacesARegularFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".local/bin/theme"), "#!/bin/sh\necho old\n")

	quiet(t, func() {
		if err := linkThemeName(); err != nil {
			t.Errorf("linkThemeName: %v", err)
		}
	})
	if _, err := os.Readlink(filepath.Join(home, ".local/bin/theme")); err != nil {
		t.Errorf("a regular file was left in place: %v", err)
	}
}

func TestBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("copies a file beside itself", func(t *testing.T) {
		writeFile(t, filepath.Join(home, ".tmux.conf"), "set -g mouse on\n")

		if err := backup(".tmux.conf"); err != nil {
			t.Fatalf("backup: %v", err)
		}
		got := readFile(t, filepath.Join(home, ".tmux.conf.before-setup"))
		if got != "set -g mouse on\n" {
			t.Errorf("backup content = %q", got)
		}
		// The original has to survive; chezmoi overwrites it next.
		if readFile(t, filepath.Join(home, ".tmux.conf")) != "set -g mouse on\n" {
			t.Error("backup modified the original")
		}
	})

	t.Run("copies a directory", func(t *testing.T) {
		writeFile(t, filepath.Join(home, ".config/nvim/init.lua"), "-- mine\n")

		if err := backup(".config/nvim"); err != nil {
			t.Fatalf("backup: %v", err)
		}
		got := readFile(t, filepath.Join(home, ".config/nvim.before-setup/init.lua"))
		if got != "-- mine\n" {
			t.Errorf("backup content = %q", got)
		}
	})

	// Nothing there is not a failure: the path may be new on this machine.
	t.Run("missing path is a no-op", func(t *testing.T) {
		if err := backup(".config/does-not-exist"); err != nil {
			t.Errorf("backup of a missing path returned %v, want nil", err)
		}
	})
}

func TestAURHelper(t *testing.T) {
	dir := t.TempDir()

	t.Run("none installed", func(t *testing.T) {
		t.Setenv("PATH", dir)
		if got := aurHelper(); got != "" {
			t.Errorf("aurHelper = %q, want \"\"", got)
		}
	})

	t.Run("yay alone", func(t *testing.T) {
		fakeBin(t, dir, "yay")
		t.Setenv("PATH", dir)
		if got := aurHelper(); got != "yay" {
			t.Errorf("aurHelper = %q, want %q", got, "yay")
		}
	})

	// paru is preferred when both are present.
	t.Run("paru wins over yay", func(t *testing.T) {
		fakeBin(t, dir, "paru")
		t.Setenv("PATH", dir)
		if got := aurHelper(); got != "paru" {
			t.Errorf("aurHelper = %q, want %q", got, "paru")
		}
	})
}

func TestInstallAURWithoutAHelper(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := installAUR([]string{"quickshell"})
	if err == nil {
		t.Fatal("installAUR with no helper returned nil error")
	}
	// The message has to name the packages, since the user has to get them
	// some other way.
	if !strings.Contains(err.Error(), "quickshell") {
		t.Errorf("error %q does not name the packages", err)
	}
}

func TestInstallNothingIsANoOp(t *testing.T) {
	// Neither should shell out at all, which is what makes a fully-installed
	// machine cheap to re-run.
	t.Setenv("PATH", t.TempDir())
	if err := installPackages(nil); err != nil {
		t.Errorf("installPackages(nil) = %v, want nil", err)
	}
	if err := installAUR(nil); err != nil {
		t.Errorf("installAUR(nil) = %v, want nil", err)
	}
	if err := applyPaths(nil); err != nil {
		t.Errorf("applyPaths(nil) = %v, want nil", err)
	}
}

func TestMissingPackages(t *testing.T) {
	if !sys.Have("pacman") {
		t.Skip("no pacman on this machine")
	}
	got := missing([]string{"hydra-definitely-not-a-real-package"})
	if len(got) != 1 {
		t.Errorf("missing = %v, want the nonexistent package reported", got)
	}
}

// -- execute -----------------------------------------------------------------

// With nothing to install, execute runs only the bootstrap — which is the path
// a second `hydra sync` takes on an already-configured machine.
func TestExecuteRunsPendingStepsAndSkipsDoneOnes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var ran []string
	selected := []Component{{
		Key: "test",
		Post: []Step{
			{
				Name:  "already done",
				Check: func() bool { return true },
				Run:   func() error { ran = append(ran, "done"); return nil },
			},
			{
				Name:  "still pending",
				Check: func() bool { return false },
				Run:   func() error { ran = append(ran, "pending"); return nil },
			},
		},
	}}

	out := quiet(t, func() {
		if err := execute(selected, nil, nil, nil, nil); err != nil {
			t.Errorf("execute: %v", err)
		}
	})

	if len(ran) != 1 || ran[0] != "pending" {
		t.Errorf("ran %v, want only the pending step", ran)
	}
	if !strings.Contains(out, "already done") {
		t.Errorf("the skipped step was not reported:\n%s", out)
	}
}

// One failed bootstrap step must not abort the rest of the setup — the other
// components are still worth installing.
func TestExecuteContinuesAfterAFailedStep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var ran []string
	selected := []Component{{
		Key: "test",
		Post: []Step{
			{
				Name:  "explodes",
				Check: func() bool { return false },
				Run:   func() error { return errors.New("deliberate failure") },
			},
			{
				Name:  "runs anyway",
				Check: func() bool { return false },
				Run:   func() error { ran = append(ran, "second"); return nil },
			},
		},
	}}

	out := quiet(t, func() {
		if err := execute(selected, nil, nil, nil, nil); err != nil {
			t.Errorf("execute returned %v; a failed bootstrap step should not abort it", err)
		}
	})

	if len(ran) != 1 {
		t.Errorf("the step after the failure did not run: %v", ran)
	}
	if !strings.Contains(out, "deliberate failure") {
		t.Errorf("the failure was not reported:\n%s", out)
	}
}

func TestExecuteBacksUpBeforeApplying(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".tmux.conf"), "mine\n")

	// A chezmoi that refuses, so the apply fails — the backup still has to have
	// happened first, which is the ordering that makes the prompt honest.
	stubs := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubs, "chezmoi"),
		[]byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	quiet(t, func() {
		if err := execute(nil, nil, nil, []string{".tmux.conf"}, []string{".tmux.conf"}); err == nil {
			t.Error("execute succeeded despite chezmoi apply failing")
		}
	})

	if got := readFile(t, filepath.Join(home, ".tmux.conf.before-setup")); got != "mine\n" {
		t.Errorf("backup = %q, want it taken before the apply was attempted", got)
	}
}

// -- helpers -----------------------------------------------------------------

func fakeBin(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
}
