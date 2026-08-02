package setup

// The two commands that serve the actual point of this tool: keeping several
// machines that are meant to be identical from drifting apart.
//
//	hydra status   what has drifted on this machine, changing nothing
//	hydra sync     pull the config repo and put this machine back in line

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// DefaultConfigRepo is where `hydra init` clones from when given no argument.
const DefaultConfigRepo = "git@github.com:jrdriscoll17/dotfiles.git"

// Init prepares a machine that has never run hydra: chezmoi, the config repo,
// and the `theme` symlink that makes this binary answer to both names.
func Init(repo string) error {
	if repo == "" {
		repo = DefaultConfigRepo
	}

	if !chezmoiReady() {
		fmt.Println(dimStyle.Render("installing chezmoi..."))
		if err := installChezmoi(); err != nil {
			return fmt.Errorf("installing chezmoi: %w", err)
		}
	}

	source := sys.InHome("dotfiles")
	if !sys.Exists(source) {
		fmt.Printf("%s %s\n", titleStyle.Render("▸ cloning"), repo)
		if err := sys.Run("git", "clone", repo, source); err != nil {
			return fmt.Errorf("cloning %s: %w", repo, err)
		}
	} else {
		fmt.Println(dimStyle.Render("config repo already at " + source))
	}

	if _, err := sys.Capture("chezmoi", "source-path"); err != nil {
		fmt.Println(titleStyle.Render("▸ initialising chezmoi"))
		if err := sys.Run("chezmoi", "init", "--source", source); err != nil {
			return fmt.Errorf("chezmoi init: %w", err)
		}
	}

	if err := linkThemeName(); err != nil {
		fmt.Println(warnStyle.Render("  " + err.Error()))
	}

	fmt.Println(okStyle.Render("\nready — run `" + invocation() +
		"` to choose components and install"))
	return nil
}

// invocation is how the user can actually reach this binary right now. On a
// machine hydra has never run on, `go install` has put it somewhere not yet on
// PATH — the shell config that fixes that is one of the files this is about to
// deploy — so telling them to run `hydra` is advice that does not work yet.
func invocation() string {
	if sys.Have("hydra") {
		return "hydra"
	}
	self, err := os.Executable()
	if err != nil {
		return "hydra"
	}
	if home := sys.Home(); home != "" && strings.HasPrefix(self, home+"/") {
		return "~" + strings.TrimPrefix(self, home)
	}
	return self
}

// linkThemeName puts a `theme` symlink beside this binary. `go install` only
// produces one executable, and the switcher is reached by argv[0].
func linkThemeName() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate this binary to link `theme`: %w", err)
	}
	link := sys.InHome(".local/bin/theme")
	if err := os.MkdirAll(sys.InHome(".local/bin"), 0o755); err != nil {
		return err
	}
	if sys.Exists(link) {
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	if err := os.Symlink(self, link); err != nil {
		return fmt.Errorf("linking %s -> %s: %w", link, self, err)
	}
	fmt.Printf("  linked %s → %s\n", link, self)
	return nil
}

// Status reports what has drifted on this machine without changing anything.
func Status() error {
	if !chezmoiReady() {
		return fmt.Errorf("chezmoi is not installed; run `hydra init` first")
	}

	laptop := isLaptop()
	selected := loadSelection(forHost(catalog(), laptop))

	fmt.Println(titleStyle.Render("  hydra status"))
	fmt.Println(dimStyle.Render(fmt.Sprintf("  %s · %s\n", hostname(), archLabel())))

	drift, err := survey(selected)
	if err != nil {
		return err
	}

	printPlan(selected, drift.pacman, drift.aur, drift.fresh, drift.conflicts)
	printDetail("packages missing", append(drift.pacman, drift.aur...))
	printDetail("config not yet on this machine", drift.fresh)
	printDetail("config that differs from the repo", drift.conflicts)
	printDetail("bootstrap steps pending", pendingSteps(selected))

	if drift.clean() && len(pendingSteps(selected)) == 0 {
		fmt.Println(okStyle.Render("\nin sync"))
	} else {
		fmt.Println(dimStyle.Render("\nrun `hydra sync` to bring this machine back in line"))
	}
	return nil
}

// Sync pulls the config repo and reapplies, so a change made on one machine
// lands on this one. Components come from what this machine opted into, not
// from a fresh prompt.
func Sync() error {
	if !chezmoiReady() {
		return fmt.Errorf("chezmoi is not installed; run `hydra init` first")
	}

	source, err := sys.Capture("chezmoi", "source-path")
	if err != nil {
		return fmt.Errorf("no chezmoi source dir; run `hydra init` first")
	}

	fmt.Println(titleStyle.Render("▸ Pulling " + source))
	if err := sys.Run("git", "-C", source, "pull", "--ff-only"); err != nil {
		// A dirty or diverged source is the user's to resolve; everything after
		// this still works against whatever is checked out.
		fmt.Println(warnStyle.Render("  pull failed — continuing with the current checkout"))
	}

	laptop := isLaptop()
	selected := loadSelection(forHost(catalog(), laptop))
	fmt.Printf("  components: %s\n", strings.Join(names(selected), ", "))

	drift, err := survey(selected)
	if err != nil {
		return err
	}
	printPlan(selected, drift.pacman, drift.aur, drift.fresh, drift.conflicts)

	decisions, err := resolveConflicts(drift.conflicts)
	if err != nil {
		return err
	}
	apply, backups := partition(drift.fresh, decisions)

	return execute(selected, drift.pacman, drift.aur, apply, backups)
}

// drift is what separates this machine from the repo.
type drift struct {
	pacman, aur      []string
	fresh, conflicts []string
}

func (d drift) clean() bool {
	return len(d.pacman) == 0 && len(d.aur) == 0 &&
		len(d.fresh) == 0 && len(d.conflicts) == 0
}

// survey works out everything that needs doing for the given components.
func survey(selected []Component) (drift, error) {
	var d drift
	var paths []string
	for _, c := range selected {
		d.pacman = append(d.pacman, missing(c.Packages)...)
		d.aur = append(d.aur, missing(c.AUR)...)
		paths = append(paths, c.Paths...)
	}

	states, err := scan(paths)
	if err != nil {
		return d, fmt.Errorf("reading chezmoi status: %w", err)
	}
	for p, s := range states {
		switch s {
		case StateConflict:
			d.conflicts = append(d.conflicts, p)
		case StateNew:
			d.fresh = append(d.fresh, p)
		}
	}
	sort.Strings(d.conflicts)
	sort.Strings(d.fresh)
	return d, nil
}

func names(components []Component) []string {
	var out []string
	for _, c := range components {
		out = append(out, c.Name)
	}
	return out
}
