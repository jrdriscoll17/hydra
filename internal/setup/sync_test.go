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

// The bootstrap's chicken-and-egg: `go install` writes to $GOPATH/bin, which is
// only put on PATH by the shell config hydra has not deployed yet. So after
// `hydra init` on a fresh machine, telling the user to run `hydra` is advice
// that does not work — it has to name a path that does.
func TestInvocation(t *testing.T) {
	t.Run("names a reachable path when hydra is not on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		got := invocation()
		if got == "hydra" {
			t.Fatal("invocation = \"hydra\", which is exactly the command that does " +
				"not resolve yet on a freshly bootstrapped machine")
		}
		// Either an absolute path or a ~-relative one; both are runnable.
		if !strings.HasPrefix(got, "/") && !strings.HasPrefix(got, "~/") {
			t.Errorf("invocation = %q, want something the user can actually type", got)
		}
	})

	t.Run("prefers the bare name once it is on PATH", func(t *testing.T) {
		dir := t.TempDir()
		fakeBin(t, dir, "hydra")
		t.Setenv("PATH", dir)

		if got := invocation(); got != "hydra" {
			t.Errorf("invocation = %q, want %q once it resolves", got, "hydra")
		}
	})

	t.Run("abbreviates the home directory", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		self, err := os.Executable()
		if err != nil {
			t.Skip("cannot determine this executable")
		}
		// Pretend the binary sits under HOME, as ~/go/bin/hydra does.
		t.Setenv("HOME", filepath.Dir(filepath.Dir(self)))

		got := invocation()
		if !strings.HasPrefix(got, "~/") {
			t.Errorf("invocation = %q, want it shortened to ~/…", got)
		}
	})
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

// -- chezmoi readiness -------------------------------------------------------

// stubChezmoi puts a fake `chezmoi` first on PATH that prints sourcePath and
// exits 0, which is what the real one does even with no config file.
func stubChezmoi(t *testing.T, sourcePath string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n  source-path) echo " + sourcePath + " ;;\n" +
		"  *) echo 'chezmoi: no config' >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "chezmoi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The bug this pins: `chezmoi source-path` exits 0 on a machine with no config
// at all, reporting the default ~/.local/share/chezmoi. Treating that as "set
// up" skipped `chezmoi init` on exactly the fresh machines that needed it, and
// the first command to read the source died with a bare "exit status 1".
func TestChezmoiInitialised(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := filepath.Join(home, "dotfiles")
	mkdir(t, source)

	t.Run("succeeding but pointing at the default location is not set up", func(t *testing.T) {
		stubChezmoi(t, filepath.Join(home, ".local/share/chezmoi"))
		if chezmoiInitialised(source) {
			t.Error("chezmoiInitialised = true while chezmoi points at its own default, " +
				"not at the config repo — this is the state that produced " +
				"\"reading chezmoi status: exit status 1\"")
		}
	})

	t.Run("pointing at the config repo is set up", func(t *testing.T) {
		stubChezmoi(t, source)
		if !chezmoiInitialised(source) {
			t.Error("chezmoiInitialised = false while chezmoi points at the config repo")
		}
	})

	// Pointing at a directory that is not there is no better than not being
	// configured at all.
	t.Run("pointing at a missing directory is not set up", func(t *testing.T) {
		stubChezmoi(t, filepath.Join(home, "gone"))
		if chezmoiInitialised(filepath.Join(home, "gone")) {
			t.Error("chezmoiInitialised = true for a source directory that does not exist")
		}
	})

	t.Run("no chezmoi at all", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if chezmoiInitialised(source) {
			t.Error("chezmoiInitialised = true with no chezmoi installed")
		}
	})
}

// A bare "exit status 1" from a tool hydra only drives is not a usable report;
// the reason is on stderr and has to survive.
func TestScanErrorExplainsItself(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// source-path answers the default, status fails — the real fresh-machine state.
	stubChezmoi(t, filepath.Join(home, ".local/share/chezmoi"))

	_, err := scan([]string{".config/nvim"})
	if err == nil {
		t.Fatal("scan succeeded against an uninitialised chezmoi")
	}
	if !strings.Contains(err.Error(), "hydra init") {
		t.Errorf("error %q does not tell the user what to do about it", err)
	}
}

// One unresolvable package must not stop the rest installing. `pacman -S`
// fails the whole transaction on a single unknown target, so a name that
// upstream has renamed or moved to the AUR would otherwise leave a fresh
// machine with nothing installed — after the user had already authenticated.
func TestInstallPackagesSkipsUnknownOnes(t *testing.T) {
	if !sys.Have("pacman") {
		t.Skip("no pacman on this machine")
	}

	// No sudo on PATH, so the install itself cannot run; what is under test is
	// that the unknown name is reported and does not abort the attempt.
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out := quiet(t, func() {
		_ = installPackages([]string{"hydra-not-a-real-package"})
	})
	if !strings.Contains(out, "hydra-not-a-real-package") {
		t.Errorf("the unresolvable package was not reported:\n%s", out)
	}
}

// With nothing resolvable there is nothing to do, so it must not shell out to
// sudo at all — that would prompt for a password to install an empty list.
func TestInstallPackagesDoesNothingWhenAllAreUnknown(t *testing.T) {
	if !sys.Have("pacman") {
		t.Skip("no pacman on this machine")
	}
	// A sudo that fails loudly if it is called.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sudo"),
		[]byte("#!/bin/sh\necho SUDO-WAS-CALLED\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out := quiet(t, func() {
		if err := installPackages([]string{"hydra-not-a-real-package"}); err != nil {
			t.Errorf("installPackages = %v, want nil when there is nothing to install", err)
		}
	})
	if strings.Contains(out, "SUDO-WAS-CALLED") {
		t.Error("sudo was invoked with an empty package list")
	}
}
