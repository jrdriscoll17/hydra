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
	"sync"

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

	source := configSource()
	if !sys.Exists(source) {
		fmt.Printf("%s %s\n", titleStyle.Render("▸ cloning"), repo)
		if err := sys.Run("git", "clone", repo, source); err != nil {
			return fmt.Errorf("cloning %s: %w", repo, err)
		}
	} else {
		fmt.Println(dimStyle.Render("config repo already at " + source))
	}

	if !chezmoiInitialised(source) {
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

	// Once, not once per use. Every bootstrap check shells out or hashes
	// something — `fisher list` starts a fish, the render stamp is a sha256 of
	// this binary — and this report asks for the answer three times over.
	pending := pendingSteps(selected)

	printPlan(selected, drift, pending)
	printDetail("packages missing", append(drift.pacman, drift.aur...))
	printDetail("config not yet on this machine", drift.fresh)
	printDetail("config that differs from the repo", drift.conflicts)
	printDetail("theme-owned lines, left alone (this machine's theme differs "+
		"from the committed one)", drift.themed)
	printDetail("not in the repo, in a directory the repo owns (left over from "+
		"an older version of it, and still being loaded)", drift.stray)
	printDetail("bootstrap steps pending", pending)

	// The system checks belong here more than anywhere: they are the part of
	// this report that comes from asking the machine rather than from comparing
	// files, and they were previously only ever printed at the end of a full
	// install — which is not the command anyone runs when something is wrong.
	failed := printChecks(systemChecks(selectedKeys(selected)))

	if drift.clean() && len(pending) == 0 && failed == 0 {
		fmt.Println(okStyle.Render("\nin sync"))
	} else {
		fmt.Println(dimStyle.Render("\nrun `hydra sync` to bring this machine back in line"))
	}
	return nil
}

// selectedKeys is the component set in the form systemChecks wants it.
func selectedKeys(selected []Component) map[string]bool {
	picked := map[string]bool{}
	for _, c := range selected {
		picked[c.Key] = true
	}
	return picked
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
	printPlan(selected, drift, pendingSteps(selected))

	decisions, err := resolveConflicts(drift.conflicts)
	if err != nil {
		return err
	}
	apply, backups := partition(drift.fresh, decisions)

	leftovers, err := resolveStrays(drift.stray)
	if err != nil {
		return err
	}

	return execute(selected, drift.pacman, drift.aur, apply, backups, leftovers)
}

// drift is what separates this machine from the repo.
type drift struct {
	pacman, aur      []string
	fresh, conflicts []string

	// themed are files whose only difference from the repo is a setting the
	// theme switcher rewrites — expected on any machine running a theme other
	// than whichever was committed, and not something to ask about.
	themed []string

	// stray are files in the repo's own directories that the repo does not
	// contain: config from an older version of it that chezmoi left behind and
	// nothing else looks for. See stray.go.
	stray []string
}

func (d drift) clean() bool {
	return len(d.pacman) == 0 && len(d.aur) == 0 &&
		len(d.fresh) == 0 && len(d.conflicts) == 0 && len(d.stray) == 0
}

// survey works out everything that needs doing for the given components.
//
// The three questions it asks the machine — what pacman has, what chezmoi would
// change, and what is left over in the directories the repo owns — do not depend
// on each other, and each is a process start or two. Asking them together is
// most of the difference between a status that reads instantly and one you wait
// for: on a machine that is already in sync, this is the whole command.
func survey(selected []Component) (drift, error) {
	var d drift
	var pkgs, aur, paths []string
	for _, c := range selected {
		pkgs = append(pkgs, c.Packages...)
		aur = append(aur, c.AUR...)
		paths = append(paths, c.Paths...)
	}

	var (
		wg        sync.WaitGroup
		installed map[string]bool
		states    map[string]FileState
		scanErr   error
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		installed = installedPackages()
	}()
	go func() {
		defer wg.Done()
		states, scanErr = scan(paths)
	}()
	go func() {
		defer wg.Done()
		d.stray = strays(selected)
	}()
	wg.Wait()

	if scanErr != nil {
		return d, fmt.Errorf("reading chezmoi status: %w", scanErr)
	}

	d.pacman = missing(pkgs, installed)
	d.aur = missing(aur, installed)

	for p, s := range states {
		switch s {
		case StateConflict:
			d.conflicts = append(d.conflicts, p)
		case StateNew:
			d.fresh = append(d.fresh, p)
		}
	}
	// Theme-owned drift is expected and self-correcting: the render at the end
	// of the run rewrites those lines anyway, so applying the repo's version
	// first would only undo and redo the same edit.
	d.conflicts, d.themed = splitThemeDrift(d.conflicts)

	sort.Strings(d.conflicts)
	sort.Strings(d.fresh)
	sort.Strings(d.themed)
	return d, nil
}

func names(components []Component) []string {
	var out []string
	for _, c := range components {
		out = append(out, c.Name)
	}
	return out
}
