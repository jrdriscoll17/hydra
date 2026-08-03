package setup

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/jrdriscoll17/hydra/internal/sys"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	dimStyle   = lipgloss.NewStyle().Faint(true)
)

// Resolution is what to do about a config file that already exists and differs.
type Resolution string

const (
	Overwrite Resolution = "overwrite"
	BackupTo  Resolution = "backup"
	Keep      Resolution = "keep"
	ShowDiff  Resolution = "diff"
)

func Run() error {
	fmt.Println(titleStyle.Render("  hydra"))
	fmt.Println(dimStyle.Render(fmt.Sprintf("  %s · %s\n", hostname(), archLabel())))

	if err := preflight(); err != nil {
		return err
	}

	laptop := isLaptop()
	available := forHost(catalog(), laptop)

	selected, err := chooseComponents(available, laptop)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Println(dimStyle.Render("nothing selected — done"))
		return nil
	}

	d, err := survey(selected)
	if err != nil {
		return err
	}
	printPlan(selected, d.pacman, d.aur, d.fresh, d.conflicts, d.stray)

	decisions, err := resolveConflicts(d.conflicts)
	if err != nil {
		return err
	}
	apply, backups := partition(d.fresh, decisions)

	leftovers, err := resolveStrays(d.stray)
	if err != nil {
		return err
	}

	var proceed bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Apply this plan?").
			Description(planSummary(d.pacman, d.aur, apply, backups, leftovers)).
			Affirmative("Install").
			Negative("Cancel").
			Value(&proceed),
	)).Run(); err != nil {
		return err
	}
	if !proceed {
		fmt.Println(dimStyle.Render("cancelled — nothing was changed"))
		return nil
	}

	if err := execute(selected, d.pacman, d.aur, apply, backups, leftovers); err != nil {
		return err
	}

	// Remember the choice so `hydra sync` keeps exactly these current.
	if err := saveSelection(selected); err != nil {
		fmt.Println(warnStyle.Render("  could not record the component selection: " + err.Error()))
	}
	return nil
}

// preflight makes sure the things the installer itself needs are present.
func preflight() error {
	if !chezmoiReady() {
		var install bool
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title("chezmoi is not installed").
				Description("It places the config files. Install it to ~/.local/bin?").
				Value(&install),
		)).Run(); err != nil {
			return err
		}
		if !install {
			return errors.New("chezmoi is required to place configs")
		}
		if err := installChezmoi(); err != nil {
			return fmt.Errorf("installing chezmoi: %w", err)
		}
	}

	// A source dir that has never been initialised has no config file, so
	// templates cannot render and every later chezmoi call fails.
	source := configSource()
	if !chezmoiInitialised(source) {
		if !sys.Exists(source) {
			return fmt.Errorf("no config repo at %s; run `hydra init` first", source)
		}
		fmt.Println(dimStyle.Render("initialising chezmoi from " + source + "..."))
		if err := sys.Run("chezmoi", "init", "--source", source); err != nil {
			return fmt.Errorf("chezmoi init: %w", err)
		}
	}
	return nil
}

func chooseComponents(available []Component, laptop bool) ([]Component, error) {
	kind := "desktop"
	if laptop {
		kind = "laptop (battery detected)"
	}

	opts := make([]huh.Option[string], 0, len(available))
	for _, c := range available {
		label := fmt.Sprintf("%-20s %s", c.Name, dimStyle.Render(c.Desc))
		opts = append(opts, huh.NewOption(label, c.Key).Selected(c.Default))
	}

	var keys []string
	err := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("What should this machine have?").
			Description(fmt.Sprintf("Detected: %s. Space toggles, enter confirms.", kind)).
			Options(opts...).
			Value(&keys),
	)).WithHeight(min(len(opts)+6, 20)).Run()
	if err != nil {
		return nil, err
	}

	picked := map[string]bool{}
	for _, k := range keys {
		picked[k] = true
	}
	var out []Component
	for _, c := range available {
		if picked[c.Key] {
			out = append(out, c)
		}
	}
	return out, nil
}

// resolveConflicts asks, per file that already exists and differs, what to do.
// Answering for one file offers to apply the same choice to the rest, because
// a fresh machine with a pre-existing config tends to want one blanket answer.
func resolveConflicts(conflicts []string) (map[string]Resolution, error) {
	decisions := map[string]Resolution{}
	if len(conflicts) == 0 {
		return decisions, nil
	}

	fmt.Println(warnStyle.Render(fmt.Sprintf("\n%d file(s) already exist and differ from the repo:", len(conflicts))))
	for _, p := range conflicts {
		fmt.Println("  " + p)
	}
	fmt.Println()

	var applyAll bool
	var blanket Resolution

	for _, path := range conflicts {
		if applyAll {
			decisions[path] = blanket
			continue
		}

		for {
			var choice Resolution
			err := huh.NewForm(huh.NewGroup(
				huh.NewSelect[Resolution]().
					Title(path).
					Description("This file exists already and does not match the repo.").
					Options(
						huh.NewOption("Show the diff first", ShowDiff),
						huh.NewOption("Keep mine (skip this file)", Keep),
						huh.NewOption("Back it up, then take the repo's", BackupTo),
						huh.NewOption("Overwrite with the repo's", Overwrite),
					).
					Value(&choice),
			)).Run()
			if err != nil {
				return nil, err
			}

			if choice == ShowDiff {
				fmt.Println()
				_ = showDiff(path)
				fmt.Println()
				continue
			}

			decisions[path] = choice

			// Only worth asking when there are others still undecided.
			if len(decisions) < len(conflicts) {
				var rest bool
				if err := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Apply %q to the remaining %d file(s)?",
							choice, len(conflicts)-len(decisions))).
						Value(&rest),
				)).Run(); err != nil {
					return nil, err
				}
				if rest {
					applyAll, blanket = true, choice
				}
			}
			break
		}
	}
	return decisions, nil
}

// resolveStrays asks what to do about files in the repo's own directories that
// the repo does not contain, and returns the ones to move aside.
//
// One question for the lot rather than one per file, unlike a conflict: there
// is no per-file judgement to make here. A conflict is two versions of a file
// the user might have edited on purpose; a stray is a file the repo dropped,
// and the only reason to keep one is that it was never the repo's to begin
// with — in which case its directory should not have been marked exclusive.
func resolveStrays(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	fmt.Println(warnStyle.Render(fmt.Sprintf(
		"\n%d file(s) sit in directories the repo owns but are not in it:", len(paths))))
	for _, p := range paths {
		fmt.Println("  " + p)
	}
	fmt.Println(dimStyle.Render(
		"  These are left over from an older version of the repo. chezmoi never\n" +
			"  removes them, and anything that loads a whole directory — lazy.nvim,\n" +
			"  Quickshell, Hyprland — still loads them."))

	var move bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Move them aside?").
			Description("Each is renamed to <name>.before-setup. Nothing is deleted.").
			Affirmative("Move aside").
			Negative("Leave them").
			Value(&move),
	)).Run(); err != nil {
		return nil, err
	}
	if !move {
		return nil, nil
	}
	return paths, nil
}

// partition turns the conflict decisions into the list of paths to apply and
// the list to back up first. Files the user kept are simply left out.
func partition(fresh []string, decisions map[string]Resolution) (apply, backups []string) {
	apply = append(apply, fresh...)
	for path, d := range decisions {
		switch d {
		case Overwrite:
			apply = append(apply, path)
		case BackupTo:
			apply = append(apply, path)
			backups = append(backups, path)
		case Keep:
			// deliberately not applied
		}
	}
	sort.Strings(apply)
	sort.Strings(backups)
	return apply, backups
}

func execute(selected []Component, pacman, aur, apply, backups, leftovers []string) error {
	step := func(name string) { fmt.Println("\n" + titleStyle.Render("▸ "+name)) }

	if len(pacman) > 0 {
		step("Installing packages")
		if err := installPackages(pacman); err != nil {
			return fmt.Errorf("pacman: %w", err)
		}
	}
	if len(aur) > 0 {
		step("Installing AUR packages")
		if err := installAUR(aur); err != nil {
			// Not fatal: the rest of the setup is still worth completing.
			fmt.Println(errStyle.Render("  " + err.Error()))
		}
	}

	if len(backups) > 0 {
		step("Backing up existing files")
		for _, p := range backups {
			if err := backup(p); err != nil {
				return fmt.Errorf("backing up %s: %w", p, err)
			}
			fmt.Printf("  %s → %s\n", p, p+".before-setup")
		}
	}

	// Before the configs land, not after: the bootstrap at the end of this run
	// starts the editor and re-renders the theme, and both should see the
	// directory the repo describes rather than the one with the leftovers still
	// in it.
	if len(leftovers) > 0 {
		step("Moving leftovers aside")
		for _, p := range leftovers {
			dst, err := clear(p)
			if err != nil {
				// Not fatal. A leftover that could not be moved leaves the
				// machine in the state it was already in, and the rest of the
				// run still improves on it.
				fmt.Println(errStyle.Render("  " + err.Error()))
				continue
			}
			fmt.Printf("  %s → %s\n", p, filepath.Base(dst))
		}
	}

	if len(apply) > 0 {
		step("Applying configs")
		if err := applyPaths(apply); err != nil {
			return fmt.Errorf("chezmoi apply: %w", err)
		}
		fmt.Printf("  %d path(s) applied\n", len(apply))
	}

	step("Bootstrapping")
	for _, c := range selected {
		for _, s := range c.Post {
			if s.Check != nil && s.Check() {
				fmt.Printf("  %s %s\n", okStyle.Render("✓"), dimStyle.Render(s.Name+" (already done)"))
				continue
			}
			fmt.Printf("  → %s\n", s.Name)
			if err := s.Run(); err != nil {
				// One failed bootstrap should not abort the whole setup.
				fmt.Println(errStyle.Render("    failed: " + err.Error()))
			}
		}
	}

	report(selected)
	return nil
}

func report(selected []Component) {
	printChecks(systemChecks(selectedKeys(selected)))
	fmt.Println(okStyle.Render("\nsetup complete"))
}

// printChecks lists the system checks and spells out the fix for any that
// failed, returning how many did.
func printChecks(checks []Check) int {
	if len(checks) == 0 {
		return 0
	}

	var failed []Check
	fmt.Println("\n" + titleStyle.Render("▸ System checks"))
	for _, c := range checks {
		mark := okStyle.Render("✓")
		if !c.OK {
			mark = warnStyle.Render("!")
			failed = append(failed, c)
		}
		fmt.Printf("  %s %s\n", mark, c.Name)
	}

	if len(failed) > 0 {
		fmt.Println(warnStyle.Render("\nStill to do by hand:"))
		for _, c := range failed {
			fmt.Printf("  %s\n    %s\n", c.Name, dimStyle.Render(c.Fix))
		}
	}
	return len(failed)
}

// -- presentation ------------------------------------------------------------

func printPlan(selected []Component, pacman, aur, fresh, conflicts, stray []string) {
	fmt.Println("\n" + titleStyle.Render("▸ Plan"))
	names := make([]string, 0, len(selected))
	for _, c := range selected {
		names = append(names, c.Name)
	}
	fmt.Printf("  components : %s\n", strings.Join(names, ", "))
	fmt.Printf("  packages   : %s\n", countLabel(len(pacman)+len(aur), "to install", "already installed"))
	fmt.Printf("  new files  : %s\n", countLabel(len(fresh), "to create", "none"))
	fmt.Printf("  conflicts  : %s\n", countLabel(len(conflicts), "need a decision", "none"))
	fmt.Printf("  leftovers  : %s\n", countLabel(len(stray), "not in the repo", "none"))
	fmt.Printf("  bootstrap  : %s\n", countLabel(len(pendingSteps(selected)), "step(s) to run", "nothing pending"))
}

// pendingSteps lists the post-install work that has not already been done.
func pendingSteps(selected []Component) []string {
	var out []string
	for _, c := range selected {
		for _, s := range c.Post {
			if s.Check == nil || !s.Check() {
				out = append(out, s.Name)
			}
		}
	}
	return out
}

func planSummary(pacman, aur, apply, backups, leftovers []string) string {
	var b strings.Builder
	if n := len(pacman) + len(aur); n > 0 {
		fmt.Fprintf(&b, "install %d package(s)\n", n)
	}
	if len(apply) > 0 {
		fmt.Fprintf(&b, "write %d config path(s)\n", len(apply))
	}
	if len(backups) > 0 {
		fmt.Fprintf(&b, "back up %d existing file(s) first\n", len(backups))
	}
	if len(leftovers) > 0 {
		fmt.Fprintf(&b, "move %d leftover file(s) aside\n", len(leftovers))
	}
	if b.Len() == 0 {
		return "Nothing to install — this will only run the bootstrap checks."
	}
	return strings.TrimSpace(b.String())
}

func printDetail(heading string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Println("\n  " + heading + ":")
	for _, i := range items {
		fmt.Println("    " + i)
	}
}

func countLabel(n int, some, none string) string {
	if n == 0 {
		return dimStyle.Render(none)
	}
	return fmt.Sprintf("%d %s", n, some)
}

func archLabel() string {
	if isLaptop() {
		return "laptop"
	}
	return "desktop"
}
