package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArg(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })

	os.Args = []string{"hydra", "init", "git@example.com:me/dotfiles.git"}
	if got := arg(2); got != "git@example.com:me/dotfiles.git" {
		t.Errorf("arg(2) = %q", got)
	}
	// `hydra init` with no repo falls back to the default, so an absent
	// argument has to read as empty rather than panicking.
	if got := arg(3); got != "" {
		t.Errorf("arg(3) = %q, want \"\"", got)
	}

	os.Args = []string{"hydra"}
	if got := arg(1); got != "" {
		t.Errorf("arg(1) with no arguments = %q, want \"\"", got)
	}
}

// The dispatch is worth exercising as a real process: argv[0] decides whether
// this binary is the installer or the theme switcher, and that cannot be
// tested by calling main().
func buildHydra(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "hydra")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building hydra: %v\n%s", err, out)
	}
	return bin
}

// run executes the binary under a throwaway HOME so nothing touches the real
// machine, and returns its combined output and exit code.
func run(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"XDG_STATE_HOME="+t.TempDir(),
	)
	out, err := cmd.CombinedOutput()

	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s %v: %v", bin, args, err)
	}
	return string(out), code
}

func TestHelp(t *testing.T) {
	bin := buildHydra(t)

	for _, flag := range []string{"-h", "--help", "help"} {
		out, code := run(t, bin, flag)
		if code != 0 {
			t.Errorf("hydra %s exited %d, want 0", flag, code)
		}
		// Every documented subcommand should appear, or the usage text has
		// drifted from the dispatch below it.
		for _, want := range []string{"init", "status", "sync", "recolor", "theme"} {
			if !strings.Contains(out, want) {
				t.Errorf("hydra %s does not mention %q:\n%s", flag, want, out)
			}
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	bin := buildHydra(t)

	out, code := run(t, bin, "frobnicate")
	if code != 2 {
		t.Errorf("exit code = %d, want 2 for a usage error", code)
	}
	if !strings.Contains(out, "frobnicate") {
		t.Errorf("output does not name the bad command:\n%s", out)
	}
}

func TestRecolorArity(t *testing.T) {
	bin := buildHydra(t)

	for _, args := range [][]string{
		{"recolor"},
		{"recolor", "Base"},
		{"recolor", "Base", "#ffffff"},
		{"recolor", "Base", "#ffffff", "Name", "extra"},
	} {
		out, code := run(t, bin, args...)
		if code != 2 {
			t.Errorf("hydra %v exited %d, want 2\n%s", args, code, out)
		}
		if !strings.Contains(out, "usage:") {
			t.Errorf("hydra %v did not print usage:\n%s", args, out)
		}
	}
}

// Through a `theme` symlink the same binary is the switcher. This is how
// Quickshell's ThemeState.qml reaches it, so the symlink dispatch is load-
// bearing rather than a convenience.
func TestThemeSymlinkDispatch(t *testing.T) {
	bin := buildHydra(t)
	link := filepath.Join(filepath.Dir(bin), "theme")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, link, "--help")
	if code != 0 {
		t.Errorf("theme --help exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "Global theme switcher") {
		t.Errorf("through the `theme` symlink the binary did not become the switcher:\n%s", out)
	}
}

// `hydra theme <cmd>` has to reach the same place as the symlink.
func TestThemeSubcommand(t *testing.T) {
	bin := buildHydra(t)

	out, code := run(t, bin, "theme", "--help")
	if code != 0 {
		t.Errorf("hydra theme --help exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "Global theme switcher") {
		t.Errorf("output:\n%s", out)
	}
}

// status refuses to guess when the machine has never been set up, rather than
// reporting everything as drifted.
func TestStatusWithoutChezmoi(t *testing.T) {
	bin := buildHydra(t)

	cmd := exec.Command(bin, "status")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "PATH=/nonexistent")
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Errorf("hydra status succeeded with no chezmoi installed:\n%s", out)
	}
	if !strings.Contains(string(out), "hydra init") {
		t.Errorf("the error does not point at `hydra init`:\n%s", out)
	}
}
