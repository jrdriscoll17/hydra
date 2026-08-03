package setup

// Config the theme switcher rewrites in place.
//
// A handful of files are shared: they hold real settings that belong in the
// repo, and one or two lines the switcher retargets on every `theme apply`. So
// on a machine running a different theme from whichever was committed, chezmoi
// reports them modified — forever. They were listed as conflicts on every
// single sync, asking the same six questions with the same answer, and
// `hydra status` could never read clean.
//
// That drift is expected, not a conflict. Recognise it by looking at which
// lines actually differ: if every one of them is a setting the switcher owns,
// there is nothing for the user to decide and nothing worth applying — the next
// render would only put it back.

import (
	"os"
	"strings"
	"sync"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// themeOwnedKeys maps a chezmoi target path to the settings renderGTK, renderQt
// and renderBtop rewrite in it. Anything not listed here is a real difference
// and still needs a decision.
var themeOwnedKeys = map[string][]string{
	".config/gtk-3.0/settings.ini": {
		"gtk-theme-name", "gtk-icon-theme-name", "gtk-application-prefer-dark-theme",
	},
	".config/gtk-4.0/settings.ini": {
		"gtk-theme-name", "gtk-icon-theme-name", "gtk-application-prefer-dark-theme",
	},
	".gtkrc-2.0": {
		"gtk-theme-name", "gtk-icon-theme-name",
	},
	".config/qt5ct/qt5ct.conf": {
		"icon_theme", "style", "standard_dialogs",
	},
	".config/qt6ct/qt6ct.conf": {
		"icon_theme", "style", "standard_dialogs",
	},
	".config/btop/btop.conf": {
		"color_theme",
	},
}

// settingKey returns the key an INI-ish line assigns to, or "" if it does not
// assign anything. Both conventions appear in these files: `key=value` in the
// GTK and Qt configs, `key = "value"` in btop's.
func settingKey(line string) string {
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}

// contentLines splits a file into its meaningful lines, dropping blanks.
//
// Blank lines have to go: `chezmoi cat` comes back through Capture, which trims
// trailing whitespace, so a file's final newline would otherwise register as a
// difference and report every one of these files as drifting even when nothing
// had changed.
func contentLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// linesDiffer returns the meaningful lines present in one text but not the
// other, in either direction.
func linesDiffer(a, b string) []string {
	left, right := contentLines(a), contentLines(b)

	count := map[string]int{}
	for _, l := range left {
		count[l]++
	}
	for _, l := range right {
		count[l]--
	}

	var out []string
	for _, l := range append(left, right...) {
		if count[l] != 0 {
			out = append(out, l)
			count[l] = 0
		}
	}
	return out
}

// themeOwnedDrift reports whether every line differing between the repo's
// version of a file and what is on disk is a setting the theme switcher owns.
func themeOwnedDrift(path string) bool {
	keys, tracked := themeOwnedKeys[path]
	if !tracked {
		return false
	}

	want, err := sys.Capture("chezmoi", "cat", sys.InHome(path))
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(sys.InHome(path))
	if err != nil {
		return false
	}

	diff := linesDiffer(want, string(raw))
	if len(diff) == 0 {
		return false
	}
	for _, line := range diff {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		key := settingKey(line)
		if key == "" || !containsString(keys, key) {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// splitThemeDrift separates the paths whose only difference is theme-owned from
// the ones that genuinely need a decision.
//
// Asked in parallel because each answer costs a `chezmoi cat`, and these files
// come as a set — a machine on a theme other than the committed one has all six
// of them drifting at once, on every single run. Batching is not an option:
// `chezmoi cat` given several paths concatenates the contents with nothing
// between them, and not in the order it was asked.
func splitThemeDrift(conflicts []string) (real, themed []string) {
	owned := make([]bool, len(conflicts))

	var wg sync.WaitGroup
	for i, p := range conflicts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			owned[i] = themeOwnedDrift(p)
		}()
	}
	wg.Wait()

	for i, p := range conflicts {
		if owned[i] {
			themed = append(themed, p)
		} else {
			real = append(real, p)
		}
	}
	return real, themed
}
