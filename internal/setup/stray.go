package setup

// Files the repo used to own and does not any more.
//
// chezmoi writes the files its source describes and nothing else. Deleting a
// file from the repo therefore deletes it from the source, not from the
// machines that already applied it: it stays on every one of them, for good,
// and nothing says a word about it. `chezmoi status` compares what the source
// describes against the target, so a file the source has never heard of is
// invisible to it, and so to `hydra status`.
//
// Usually that is dead weight and no worse. In a directory that is loaded
// wholesale it is a live config file. lua/plugins/onedark.lua stayed behind on
// the laptop when the theme switcher took over the colorscheme; lazy.nvim
// imports every file under lua/plugins, found two specs naming the same plugin
// and merged them, and it was the leftover's unconditional
// `colorscheme("onedark")` that survived the merge. The result was a machine
// where switching themes moved the bar, the terminal, GTK, Qt and btop, and
// left the editor where it was — on one machine only, with nothing anywhere
// reporting a problem, because by every check hydra had, that machine was in
// sync.
//
// So for the directories the repo owns outright, anything else present is a
// leftover and worth naming. Nothing is deleted on the strength of it: they are
// reported by `hydra status` and moved aside, not removed, and only after being
// asked about.

import (
	"regexp"
	"sort"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// strays lists the files under the components' exclusive directories that the
// repo does not contain.
//
// `chezmoi unmanaged` is the right question to ask: it excludes both the files
// chezmoi manages and the ones .chezmoiignore names, so the switcher's own
// generated output — which is ignored precisely because it must not be in the
// repo — does not come back as a leftover. An unmanaged directory is reported
// as itself rather than recursed into, which keeps a stale tree to one line.
func strays(components []Component) []string {
	seen := map[string]bool{}
	var out []string

	for _, c := range components {
		for _, dir := range c.Exclusive {
			full := sys.InHome(dir)
			if !sys.Exists(full) {
				continue
			}
			// An error here means chezmoi could not answer, which is not the
			// same as "nothing is stray". Staying quiet is the right failure:
			// this is a report, and a wrong one is worse than none.
			listed, err := sys.Capture("chezmoi", "unmanaged", full)
			if err != nil {
				continue
			}
			for _, path := range strings.Split(listed, "\n") {
				path = strings.TrimSpace(path)
				if path == "" || seen[path] || isBackup(path) {
					continue
				}
				seen[path] = true
				out = append(out, path)
			}
		}
	}

	sort.Strings(out)
	return out
}

// backupSuffix matches what moveAside names its output, including the numbered
// form a second run produces.
var backupSuffix = regexp.MustCompile(`\.before-setup(\.\d+)?$`)

// isBackup reports whether a path is one hydra put there itself. Both the
// backup taken before overwriting a conflicting file and the leftover moved out
// of the way land beside the original, inside the very directories being
// scanned — reporting those as strays would have hydra nagging about its own
// tidying, and moving each backup to a backup of itself on every run.
func isBackup(path string) bool { return backupSuffix.MatchString(path) }

// clear moves a leftover out of the directory the repo owns.
//
// Not a delete: these were part of the setup once, and the point is to get them
// out of a directory that is loaded wholesale, not to destroy them. moveAside
// is what the conflict prompt uses to preserve a file it is about to overwrite,
// so there is one convention on disk for "this was yours, hydra moved it" — and
// it already refuses to overwrite an earlier backup.
func clear(path string) (string, error) {
	full := sys.InHome(path)
	if !sys.Exists(full) {
		return "", nil
	}
	return moveAside(full)
}
