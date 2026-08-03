package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// -- host detection ----------------------------------------------------------

// A var rather than a const only so tests can point it at a fixture; nothing
// in the program reassigns it.
var powerSupplyDir = "/sys/class/power_supply"

// isLaptop reports whether this machine has a battery. This is the same signal
// the configs should use at runtime rather than hardcoding a hostname.
func isLaptop() bool {
	entries, err := os.ReadDir(powerSupplyDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "BAT") {
			return true
		}
	}
	return false
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// -- packages ----------------------------------------------------------------

// Package queries ask pacman once per run rather than once per name.
//
// Spawning `pacman -Qq <name>` costs the same process start and database open
// whatever it is asked about — about 60ms — so the forty-odd names in the
// catalog were the bulk of what `hydra status` and `hydra sync` spent their time
// on, before either had said anything. The whole local database comes back in
// one call for the price of a single query, and membership of that list answers
// exactly the same question: `-Qq <name>` matches an installed package by its
// own name, which is what the list contains.
func installedPackages() map[string]bool { return pkgSet("-Qq") }

// knownPackages is every name the sync databases can resolve — the question
// `pacman -Si <name>` answers one package at a time, and that one is far worse
// at ~300ms a call.
func knownPackages() map[string]bool { return pkgSet("-Slq") }

// pkgSet runs a pacman listing and returns the names it printed. A pacman that
// cannot answer gives an empty set rather than a partial one; what that means is
// the caller's to decide.
func pkgSet(query string) map[string]bool {
	out, err := sys.Capture("pacman", query)
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			set[name] = true
		}
	}
	return set
}

// missing lists the packages that are not installed. A machine with no pacman
// at all reports every one of them, which is what asking a name at a time did.
func missing(pkgs []string, installed map[string]bool) []string {
	var out []string
	for _, p := range pkgs {
		if !installed[p] {
			out = append(out, p)
		}
	}
	return out
}

// aurHelper returns paru or yay, whichever is present.
func aurHelper() string {
	for _, h := range []string{"paru", "yay"} {
		if sys.Have(h) {
			return h
		}
	}
	return ""
}

// installPackages installs what pacman can resolve, and says loudly what it
// could not.
//
// `pacman -S` fails the entire transaction on a single unknown target, so one
// package that upstream has since renamed or moved to the AUR would otherwise
// stop a fresh machine installing anything at all — and only after the user has
// entered their password. Everything else is still worth having.
func installPackages(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}

	known := knownPackages()
	// A pacman that is there but answered with nothing has not told us these
	// packages do not exist — it has failed to answer, which is what an empty
	// sync database on a machine that has never run `pacman -Sy` looks like.
	// Reading that as "none of these are real" would silently skip the whole
	// install, so hand the list over and let pacman say why itself.
	if len(known) == 0 && sys.Have("pacman") {
		return sys.Run("sudo", append([]string{"pacman", "-S", "--needed"}, pkgs...)...)
	}

	var resolvable, unknown []string
	for _, p := range pkgs {
		if known[p] {
			resolvable = append(resolvable, p)
		} else {
			unknown = append(unknown, p)
		}
	}

	if len(unknown) > 0 {
		fmt.Println(warnStyle.Render("  not in the repos, skipping: " +
			strings.Join(unknown, " ")))
		fmt.Println(dimStyle.Render(
			"  (renamed, dropped, or moved to the AUR — install by hand if you want them)"))
	}
	if len(resolvable) == 0 {
		return nil
	}
	return sys.Run("sudo", append([]string{"pacman", "-S", "--needed"}, resolvable...)...)
}

func installAUR(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	helper := aurHelper()
	if helper == "" {
		return fmt.Errorf("no AUR helper (paru/yay) found; install one to get: %s",
			strings.Join(pkgs, " "))
	}
	return sys.Run(helper, append([]string{"-S", "--needed"}, pkgs...)...)
}

// -- chezmoi -----------------------------------------------------------------

func chezmoiReady() bool { return sys.Have("chezmoi") }

// configSource is where hydra keeps the chezmoi source repo.
func configSource() string { return sys.InHome("dotfiles") }

// chezmoiInitialised reports whether chezmoi is actually pointed at our config
// repo.
//
// The obvious probe — whether `chezmoi source-path` succeeds — does not work:
// it exits 0 on a machine with no config at all, printing the default
// ~/.local/share/chezmoi. Trusting that meant `chezmoi init` was skipped on
// exactly the fresh machines that needed it, and the first command to actually
// read the source failed with a bare "exit status 1".
func chezmoiInitialised(source string) bool {
	got, err := sys.Capture("chezmoi", "source-path")
	if err != nil {
		return false
	}
	return filepath.Clean(got) == filepath.Clean(source) && sys.Exists(got)
}

func installChezmoi() error {
	script := `sh -c "$(curl -fsLS get.chezmoi.io)" -- -b "$HOME/.local/bin"`
	return sys.Run("sh", "-c", script)
}

// FileState is what chezmoi would do to one target path.
type FileState int

const (
	StateClean    FileState = iota // target already matches the source
	StateNew                       // target does not exist; safe to create
	StateConflict                  // target exists and differs — needs a decision
)

// scan asks chezmoi what it would change, restricted to the given paths.
func scan(paths []string) (map[string]FileState, error) {
	out, err := sys.Capture("chezmoi", "status")
	if err != nil {
		if source := configSource(); !chezmoiInitialised(source) {
			return nil, fmt.Errorf("chezmoi is not set up against %s; "+
				"run `hydra init` first (%w)", source, err)
		}
		return nil, err
	}
	return parseStatus(out, paths), nil
}

// parseStatus reads `chezmoi status` output. It prints two status columns then
// the path; an "M" in either means the target exists and differs, "A" means it
// would be created. Anything else (deletions, scripts) is not this tool's
// business.
func parseStatus(out string, paths []string) map[string]FileState {
	states := map[string]FileState{}
	for line := range strings.SplitSeq(out, "\n") {
		if len(line) < 4 {
			continue
		}
		code, path := strings.TrimSpace(line[:2]), strings.TrimSpace(line[3:])
		if !ownedBy(path, paths) {
			continue
		}
		switch {
		case strings.Contains(code, "M"):
			states[path] = StateConflict
		case strings.Contains(code, "A"):
			states[path] = StateNew
		}
	}
	return states
}

// ownedBy reports whether a target path falls under any of the given roots.
func ownedBy(path string, roots []string) bool {
	for _, r := range roots {
		if path == r || strings.HasPrefix(path, r+"/") {
			return true
		}
	}
	return false
}

func applyPaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"apply"}, paths...)
	return sys.Run("chezmoi", args...)
}

func showDiff(path string) error { return sys.Run("chezmoi", "diff", path) }

func backup(path string) error {
	full := sys.InHome(path)
	if !sys.Exists(full) {
		return nil
	}
	dst := full + ".before-setup"
	return sys.Run("cp", "-a", full, dst)
}

// -- post-install steps ------------------------------------------------------

// -- system checks -----------------------------------------------------------

const (
	// The Material-Black GTK themes and their matching Suru-GLOW icon sets
	// both live on this one branch, so a single sparse clone gets a complete
	// base pair. Blueberry is an arbitrary pick — the recolour reads the accent
	// back off whatever base it is given.
	mbRepo   = "https://github.com/rtlewis88/rtl88-Themes"
	mbBranch = "material-black-COLORS"
	mbTheme  = "Material-Black-Blueberry-3.36"
	mbIcons  = "MB-Blueberry-Suru-GLOW"

	colloidRepo = "https://github.com/vinceliuice/Colloid-gtk-theme"
)
