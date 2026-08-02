package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// The icon index. Qt inside Quickshell has no icon theme — setting one needs a
// platform theme plugin (qt6ct or the gtk one), neither of which is installed,
// so Quickshell.iconPath() and image://icon/... only ever resolve absolute
// paths. The shell reads this index instead; see services/Icons.qml.

var iconRoots = func() []string {
	return []string{sys.InHome(".local/share/icons"), sys.InHome(".icons"), "/usr/share/icons"}
}

// Categories worth indexing: app launchers, notification icons, tray icons.
var iconCategories = []string{"apps", "status", "devices", "places", "actions", "categories"}

// iconThemeChain is a theme plus whatever it inherits, in lookup order, ending
// at hicolor.
func iconThemeChain(name string) []string {
	var chain []string
	seen := map[string]bool{}
	queue := []string{name, "hicolor"}

	for len(queue) > 0 {
		theme := queue[0]
		queue = queue[1:]
		if seen[theme] {
			continue
		}
		seen[theme] = true

		for _, root := range iconRoots() {
			dir := filepath.Join(root, theme)
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				continue
			}
			chain = append(chain, dir)
			index := filepath.Join(dir, "index.theme")
			raw, err := os.ReadFile(index)
			if err != nil {
				continue
			}
			for line := range strings.SplitSeq(string(raw), "\n") {
				if after, ok := strings.CutPrefix(line, "Inherits="); ok {
					for p := range strings.SplitSeq(after, ",") {
						queue = append(queue, strings.TrimSpace(p))
					}
				}
			}
		}
	}
	return chain
}

// sizeRank orders directories so bigger (and scalable) wins: a 512px source
// beats a 16px one for a 34px notification badge.
//
// The @2x suffix counts: "32x32@2x" holds 64px artwork, so it outranks "32x32".
// theme.py stripped the suffix instead, which tied the two and left the winner
// to whatever order the filesystem happened to return directories in — so its
// icons.json was not reproducible across machines. Ranking by effective pixels
// is both deterministic and what "bigger wins" was always meant to say.
func sizeRank(name string) (int, int) {
	if strings.HasPrefix(name, "scalable") {
		return 1, 0
	}
	head, _, _ := strings.Cut(name, "x")
	n, err := strconv.Atoi(head)
	if err != nil {
		return 0, 0
	}
	scale := 1
	if _, after, found := strings.Cut(name, "@"); found {
		digits := after
		if i := strings.IndexFunc(after, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
			digits = after[:i]
		}
		if s, err := strconv.Atoi(digits); err == nil && s > 0 {
			scale = s
		}
	}
	return 0, n * scale
}

// sortBySizeDesc mirrors Python's sorted(key=size_rank, reverse=True), which is
// stable, so equal ranks keep directory order.
func sortBySizeDesc(names []string) {
	sort.SliceStable(names, func(i, j int) bool {
		ai, aj := name2rank(names[i]), name2rank(names[j])
		if ai[0] != aj[0] {
			return ai[0] > aj[0]
		}
		return ai[1] > aj[1]
	})
}

func name2rank(n string) [2]int {
	a, b := sizeRank(n)
	return [2]int{a, b}
}

// renderIcons indexes icon-name -> file, because Qt inside Quickshell has no
// icon theme. Setting one needs a platform theme plugin (qt6ct or the gtk one),
// neither of which is installed, so Quickshell.iconPath() and image://icon/...
// only ever resolve absolute paths. The shell reads this index instead; see
// services/Icons.qml.
func renderIcons(t *Theme) error {
	out := sys.InHome(".config/quickshell/generated/icons.json")
	marker := sys.InHome(".config/quickshell/generated/icons.theme")
	iconTheme := t.GTK.Icons

	if sys.Exists(out) && sys.Exists(marker) {
		if raw, err := os.ReadFile(marker); err == nil &&
			strings.TrimSpace(string(raw)) == iconTheme {
			return nil
		}
	}

	// Each theme has its own icon set, so a naive rebuild costs ~1.6s of disk
	// walking on every single switch. Cached per icon theme, that becomes a
	// 2.5MB copy. `theme icons` clears the cache when new apps are installed.
	cache := filepath.Join(cacheDir(), "icons-"+iconTheme+".json")
	if raw, err := os.ReadFile(cache); err == nil {
		if err := sys.WriteFile(out, string(raw)); err != nil {
			return err
		}
		return sys.WriteFile(marker, iconTheme+"\n")
	}

	index := map[string]string{}
	add := func(path string) {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if _, seen := index[stem]; !seen {
			index[stem] = path
		}
	}
	isIcon := func(name string) bool {
		switch filepath.Ext(name) {
		case ".svg", ".png", ".xpm":
			return true
		}
		return false
	}

	for _, themeDir := range iconThemeChain(iconTheme) {
		// Two layouts in the wild: Papirus nests size/category, the Suru themes
		// nest category/size. Walk both by checking which half of each pair
		// names a category.
		outers := sys.Subdirs(themeDir)
		sortBySizeDesc(outers)
		for _, outer := range outers {
			outerPath := filepath.Join(themeDir, outer)

			var innerPaths []string
			if slicesContains(iconCategories, outer) {
				inners := sys.Subdirs(outerPath)
				sortBySizeDesc(inners)
				for _, in := range inners {
					innerPaths = append(innerPaths, filepath.Join(outerPath, in))
				}
			} else {
				for _, category := range iconCategories {
					innerPaths = append(innerPaths, filepath.Join(outerPath, category))
				}
			}

			for _, dir := range innerPaths {
				entries, err := os.ReadDir(dir)
				if err != nil {
					continue
				}
				for _, e := range entries {
					if isIcon(e.Name()) {
						add(filepath.Join(dir, e.Name()))
					}
				}
			}
		}
	}

	// Legacy drop-box a lot of third-party packages still use.
	if entries, err := os.ReadDir("/usr/share/pixmaps"); err == nil {
		for _, e := range entries {
			if isIcon(e.Name()) {
				add(filepath.Join("/usr/share/pixmaps", e.Name()))
			}
		}
	}

	payload, err := compactSortedJSON(index)
	if err != nil {
		return err
	}
	if err := sys.WriteFile(out, payload); err != nil {
		return err
	}
	if err := sys.WriteFile(cache, payload); err != nil {
		return err
	}
	return sys.WriteFile(marker, iconTheme+"\n")
}

func slicesContains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// compactSortedJSON matches json.dumps(separators=(",", ":"), sort_keys=True).
// Go's encoder sorts map keys and emits no spaces already; HTML escaping is the
// one difference that has to be turned off.
func compactSortedJSON(m map[string]string) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
