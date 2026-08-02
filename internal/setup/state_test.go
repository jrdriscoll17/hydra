package setup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// useStateDir isolates the recorded selection and render stamp from the real
// machine's ~/.local/state.
func useStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return filepath.Join(dir, "hydra")
}

func TestStateDirPrefersXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if got, want := stateDir(), filepath.Join(dir, "hydra"); got != want {
		t.Errorf("stateDir = %q, want %q", got, want)
	}
}

func TestStateDirFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")

	if got, want := stateDir(), filepath.Join(home, ".local/state/hydra"); got != want {
		t.Errorf("stateDir = %q, want %q", got, want)
	}
}

func TestSaveAndLoadSelection(t *testing.T) {
	useStateDir(t)
	available := []Component{
		{Key: "core", Default: true},
		{Key: "nvim", Default: true},
		{Key: "emacs", Default: false},
		{Key: "media", Default: false},
	}

	if err := saveSelection([]Component{available[0], available[2]}); err != nil {
		t.Fatalf("saveSelection: %v", err)
	}
	got := keysOf(loadSelection(available))
	if !slices.Equal(got, []string{"core", "emacs"}) {
		t.Errorf("loadSelection = %v, want [core emacs]", got)
	}
}

// A machine that has never run the installer should be kept up to date with
// the defaults, which is the sensible reading of "keep me in sync".
func TestLoadSelectionFallsBackToDefaults(t *testing.T) {
	useStateDir(t)
	available := []Component{
		{Key: "core", Default: true},
		{Key: "emacs", Default: false},
	}
	if got := keysOf(loadSelection(available)); !slices.Equal(got, []string{"core"}) {
		t.Errorf("loadSelection with no state file = %v, want the defaults [core]", got)
	}
}

func TestLoadSelectionFallsBackOnAnEmptyFile(t *testing.T) {
	dir := useStateDir(t)
	available := []Component{{Key: "core", Default: true}, {Key: "emacs"}}

	for _, content := range []string{"", "\n", "   \n\n  \n"} {
		writeFile(t, filepath.Join(dir, "components"), content)
		if got := keysOf(loadSelection(available)); !slices.Equal(got, []string{"core"}) {
			t.Errorf("loadSelection(%q) = %v, want the defaults", content, got)
		}
	}
}

// A component removed from the catalog, or one filtered out by host, must not
// resurrect itself from the state file.
func TestLoadSelectionIgnoresUnknownKeys(t *testing.T) {
	dir := useStateDir(t)
	writeFile(t, filepath.Join(dir, "components"), "core\nretired-component\nddc\n")

	available := []Component{{Key: "core"}, {Key: "nvim"}}
	if got := keysOf(loadSelection(available)); !slices.Equal(got, []string{"core"}) {
		t.Errorf("loadSelection = %v, want [core]", got)
	}
}

// The order of the result follows the catalog, not the file, so post-install
// steps still run in their intended order.
func TestLoadSelectionFollowsCatalogOrder(t *testing.T) {
	dir := useStateDir(t)
	writeFile(t, filepath.Join(dir, "components"), "theme\ncore\nnvim\n")

	available := []Component{{Key: "core"}, {Key: "nvim"}, {Key: "theme"}}
	var got []string
	for _, c := range loadSelection(available) {
		got = append(got, c.Key)
	}
	if !slices.Equal(got, []string{"core", "nvim", "theme"}) {
		t.Errorf("loadSelection = %v, want catalog order [core nvim theme]", got)
	}
}

func TestSaveSelectionRoundTripsThroughTheRealCatalog(t *testing.T) {
	useStateDir(t)
	available := forHost(catalog(), false)

	if err := saveSelection(available); err != nil {
		t.Fatalf("saveSelection: %v", err)
	}
	if got, want := keysOf(loadSelection(available)), keysOf(available); !slices.Equal(got, want) {
		t.Errorf("round trip lost components: %v, want %v", got, want)
	}
}

func TestSaveSelectionOfNothing(t *testing.T) {
	dir := useStateDir(t)
	if err := saveSelection(nil); err != nil {
		t.Fatalf("saveSelection(nil): %v", err)
	}
	// An empty record is indistinguishable from never having run, so the
	// defaults come back — which beats syncing nothing at all.
	available := []Component{{Key: "core", Default: true}}
	if got := keysOf(loadSelection(available)); !slices.Equal(got, []string{"core"}) {
		t.Errorf("loadSelection = %v, want the defaults", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "components")); err != nil {
		t.Errorf("no state file written: %v", err)
	}
}

// -- the render stamp --------------------------------------------------------

// This is what makes an upgraded hydra notice that the theme output on disk was
// written by an older build. Checking that a file merely exists would report
// "already done" forever and need a manual `theme apply`.

func TestSelfStampIsStableAndNonEmpty(t *testing.T) {
	stamp := selfStamp()
	if stamp == "" {
		t.Skip("this binary cannot read itself; renderIsCurrent assumes current")
	}
	if len(stamp) != 64 {
		t.Errorf("stamp = %q, want a 64-character sha256 hex digest", stamp)
	}
	if again := selfStamp(); again != stamp {
		t.Errorf("selfStamp varied between calls: %q then %q", stamp, again)
	}
}

func TestRenderIsCurrent(t *testing.T) {
	dir := useStateDir(t)
	if selfStamp() == "" {
		t.Skip("cannot fingerprint this binary")
	}

	t.Run("no stamp file means the render is stale", func(t *testing.T) {
		if renderIsCurrent() {
			t.Error("renderIsCurrent = true with no stamp recorded")
		}
	})

	t.Run("a matching stamp means it is current", func(t *testing.T) {
		if err := saveRenderStamp(); err != nil {
			t.Fatalf("saveRenderStamp: %v", err)
		}
		if !renderIsCurrent() {
			t.Error("renderIsCurrent = false immediately after saving the stamp")
		}
	})

	// The upgrade case: a different binary produced the output on disk.
	t.Run("a stamp from another build means it is stale", func(t *testing.T) {
		writeFile(t, filepath.Join(dir, "render-stamp"), strings.Repeat("a", 64)+"\n")
		if renderIsCurrent() {
			t.Error("renderIsCurrent = true despite the output being written by another build")
		}
	})

	t.Run("surrounding whitespace is tolerated", func(t *testing.T) {
		writeFile(t, filepath.Join(dir, "render-stamp"), "  "+selfStamp()+"  \n\n")
		if !renderIsCurrent() {
			t.Error("renderIsCurrent = false because of whitespace around the stamp")
		}
	})
}

func TestSaveRenderStampWritesTheDigest(t *testing.T) {
	dir := useStateDir(t)
	if selfStamp() == "" {
		t.Skip("cannot fingerprint this binary")
	}
	if err := saveRenderStamp(); err != nil {
		t.Fatalf("saveRenderStamp: %v", err)
	}
	got := strings.TrimSpace(readFile(t, filepath.Join(dir, "render-stamp")))
	if got != selfStamp() {
		t.Errorf("stamp file = %q, want %q", got, selfStamp())
	}
}
