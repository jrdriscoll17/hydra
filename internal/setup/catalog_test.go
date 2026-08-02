package setup

import (
	"slices"
	"strings"
	"testing"
)

// The catalog is the whole content of the tool, so its invariants are worth
// asserting: a duplicated key silently loses a component from the selection,
// and a component that is both laptop- and desktop-only appears nowhere.
func TestCatalogInvariants(t *testing.T) {
	all := catalog()
	if len(all) == 0 {
		t.Fatal("the catalog is empty")
	}

	seen := map[string]bool{}
	for _, c := range all {
		t.Run(c.Key, func(t *testing.T) {
			if c.Key == "" {
				t.Error("empty key")
			}
			if seen[c.Key] {
				t.Errorf("duplicate key %q — loadSelection would conflate the two", c.Key)
			}
			seen[c.Key] = true

			if c.Name == "" {
				t.Error("no display name")
			}
			if c.Desc == "" {
				t.Error("no description; the TUI shows it beside the name")
			}
			if c.DesktopOnly && c.LaptopOnly {
				t.Error("both DesktopOnly and LaptopOnly, so it is offered on no machine")
			}
			// A component that installs nothing, owns nothing and bootstraps
			// nothing is dead weight in the picker.
			if len(c.Packages) == 0 && len(c.AUR) == 0 && len(c.Paths) == 0 && len(c.Post) == 0 {
				t.Error("does nothing at all")
			}
			for _, s := range c.Post {
				if s.Name == "" {
					t.Error("a post-install step has no name")
				}
				if s.Run == nil {
					t.Errorf("post-install step %q has no Run function", s.Name)
				}
				// Without a check, `hydra status` cannot tell whether the step
				// is pending and re-runs would redo work.
				if s.Check == nil {
					t.Errorf("post-install step %q has no Check; it would be reported "+
						"pending forever and re-run on every sync", s.Name)
				}
			}
			for _, p := range c.Paths {
				if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
					t.Errorf("path %q is not relative to $HOME, which is what chezmoi reports", p)
				}
			}
		})
	}
}

// Two components claiming the same config path would each report the other's
// conflicts and apply it twice.
func TestCatalogPathsAreNotSharedBetweenComponents(t *testing.T) {
	owner := map[string]string{}
	for _, c := range catalog() {
		for _, p := range c.Paths {
			if prev, taken := owner[p]; taken {
				t.Errorf("path %q is claimed by both %q and %q", p, prev, c.Key)
			}
			owner[p] = c.Key
		}
	}
}

func TestCatalogPackagesAreNotDuplicatedWithinAComponent(t *testing.T) {
	for _, c := range catalog() {
		seen := map[string]bool{}
		for _, p := range append(slices.Clone(c.Packages), c.AUR...) {
			if seen[p] {
				t.Errorf("%s lists package %q twice", c.Key, p)
			}
			seen[p] = true
		}
	}
}

// kitty.conf has `include theme.conf` and errors outright if it is missing, so
// the theme component has to be on by default.
func TestThemeComponentIsOnByDefault(t *testing.T) {
	for _, c := range catalog() {
		if c.Key == "theme" {
			if !c.Default {
				t.Error("the theme component is not selected by default, but kitty.conf " +
					"includes its generated theme.conf and fails to start without it")
			}
			return
		}
	}
	t.Error("no theme component in the catalog")
}

func TestDefaults(t *testing.T) {
	all := []Component{
		{Key: "a", Default: true},
		{Key: "b", Default: false},
		{Key: "c", Default: true},
	}
	if got := keysOf(defaults(all)); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("defaults = %v, want [a c]", got)
	}
}

func TestDefaultsOnTheRealCatalog(t *testing.T) {
	got := defaults(catalog())
	if len(got) == 0 {
		t.Error("no components are selected by default")
	}
	if len(got) == len(catalog()) {
		t.Error("every component is a default; the point of opt-out is that some are not")
	}
	// The user explicitly wants to be able to leave Doom out.
	for _, c := range got {
		if c.Key == "emacs" {
			t.Error("Doom Emacs is selected by default; it is meant to be opt-in")
		}
	}
}

func TestForHost(t *testing.T) {
	all := []Component{
		{Key: "everywhere"},
		{Key: "desk", DesktopOnly: true},
		{Key: "lap", LaptopOnly: true},
	}

	t.Run("laptop", func(t *testing.T) {
		got := keysOf(forHost(all, true))
		if !slices.Equal(got, []string{"everywhere", "lap"}) {
			t.Errorf("forHost(laptop) = %v, want [everywhere lap]", got)
		}
	})

	t.Run("desktop", func(t *testing.T) {
		got := keysOf(forHost(all, false))
		if !slices.Equal(got, []string{"desk", "everywhere"}) {
			t.Errorf("forHost(desktop) = %v, want [desk everywhere]", got)
		}
	})
}

func TestForHostPreservesCatalogOrder(t *testing.T) {
	all := catalog()
	got := forHost(all, false)

	var prev int
	for _, c := range got {
		idx := slices.IndexFunc(all, func(x Component) bool { return x.Key == c.Key })
		if idx < prev {
			t.Fatalf("forHost reordered the catalog around %q", c.Key)
		}
		prev = idx
	}
}

// The laptop path has never run on real laptop hardware, so at minimum assert
// the filtering produces something sensible on both kinds of machine.
func TestRealCatalogFiltersToBothHostKinds(t *testing.T) {
	desktop := forHost(catalog(), false)
	laptop := forHost(catalog(), true)

	if len(desktop) == 0 || len(laptop) == 0 {
		t.Fatalf("desktop = %d components, laptop = %d; both must be non-empty",
			len(desktop), len(laptop))
	}
	if slices.Equal(keysOf(desktop), keysOf(laptop)) {
		t.Error("the two host kinds see an identical catalog; the host filters do nothing")
	}

	// ddc is desktop-only, laptop extras are laptop-only.
	if slices.Contains(keysOf(laptop), "ddc") {
		t.Error("the DDC component is offered on a laptop")
	}
	if slices.Contains(keysOf(desktop), "laptop") {
		t.Error("the laptop extras are offered on a desktop")
	}
	if !slices.Contains(keysOf(laptop), "laptop") {
		t.Error("the laptop extras are not offered on a laptop")
	}
}

func TestNames(t *testing.T) {
	got := names([]Component{{Name: "Core shell"}, {Name: "Neovim"}})
	if !slices.Equal(got, []string{"Core shell", "Neovim"}) {
		t.Errorf("names = %v", got)
	}
	if got := names(nil); got != nil {
		t.Errorf("names(nil) = %v, want nil", got)
	}
}
