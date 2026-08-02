package setup

// What this machine has opted into. Recorded after a successful run so `sync`
// knows what to keep current without asking again — the machines differ in which
// components they want, and that choice should survive between runs.

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

func stateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "hydra")
	}
	return sys.InHome(".local/state/hydra")
}

func stateFile() string { return filepath.Join(stateDir(), "components") }

// stampFile records which build of hydra produced the current theme output.
func stampFile() string { return filepath.Join(stateDir(), "render-stamp") }

// selfStamp fingerprints this executable. Any change to the renderers changes
// the binary, so comparing it is what lets an upgraded hydra notice that the
// theme output on disk was produced by an older one — the file simply existing
// says nothing about whether it is still what this build would write.
//
// Returns "" if the binary cannot be read, which callers treat as "assume
// current" rather than re-rendering on every run.
func selfStamp() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(self)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// renderIsCurrent reports whether the theme output was written by this build.
func renderIsCurrent() bool {
	stamp := selfStamp()
	if stamp == "" {
		return true
	}
	recorded, err := os.ReadFile(stampFile())
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(recorded)) == stamp
}

func saveRenderStamp() error {
	stamp := selfStamp()
	if stamp == "" {
		return nil
	}
	return sys.WriteFile(stampFile(), stamp+"\n")
}

// saveSelection records the component keys this machine installed.
func saveSelection(selected []Component) error {
	var keys []string
	for _, c := range selected {
		keys = append(keys, c.Key)
	}
	return sys.WriteFile(stateFile(), strings.Join(keys, "\n")+"\n")
}

// loadSelection returns the components this machine opted into last time. A
// machine that has never run the installer gets the defaults, which is the
// sensible reading of "keep me in sync" on a fresh box.
func loadSelection(available []Component) []Component {
	raw, err := os.ReadFile(stateFile())
	if err != nil {
		return defaults(available)
	}

	var keys []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			keys = append(keys, line)
		}
	}
	if len(keys) == 0 {
		return defaults(available)
	}

	var out []Component
	for _, c := range available {
		if slices.Contains(keys, c.Key) {
			out = append(out, c)
		}
	}
	return out
}
