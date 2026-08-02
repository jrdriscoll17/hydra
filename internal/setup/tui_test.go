package setup

import (
	"slices"
	"strings"
	"testing"
)

// partition turns the per-file conflict decisions into the two lists execute
// acts on. Getting this wrong overwrites a file the user chose to keep, which
// is the one outcome the conflict prompt exists to prevent.
func TestPartition(t *testing.T) {
	fresh := []string{".config/nvim/init.lua", ".config/fish/config.fish"}
	decisions := map[string]Resolution{
		".config/kitty/kitty.conf": Overwrite,
		".tmux.conf":               BackupTo,
		".gitconfig":               Keep,
	}

	apply, backups := partition(fresh, decisions)

	wantApply := []string{
		".config/fish/config.fish",
		".config/kitty/kitty.conf",
		".config/nvim/init.lua",
		".tmux.conf",
	}
	if !slices.Equal(apply, wantApply) {
		t.Errorf("apply = %v,\nwant %v", apply, wantApply)
	}
	if !slices.Equal(backups, []string{".tmux.conf"}) {
		t.Errorf("backups = %v, want [.tmux.conf]", backups)
	}

	// The whole point: a kept file is never handed to chezmoi apply.
	if slices.Contains(apply, ".gitconfig") {
		t.Error("a file the user chose to keep would be overwritten")
	}
}

func TestPartitionBackupImpliesApply(t *testing.T) {
	apply, backups := partition(nil, map[string]Resolution{".tmux.conf": BackupTo})

	if !slices.Contains(apply, ".tmux.conf") {
		t.Error("a backed-up file was not applied; backing up is pointless without taking the repo's")
	}
	if !slices.Contains(backups, ".tmux.conf") {
		t.Error("the file was applied without being backed up first")
	}
}

func TestPartitionIsDeterministic(t *testing.T) {
	fresh := []string{".b", ".a"}
	decisions := map[string]Resolution{".z": Overwrite, ".c": Overwrite, ".m": BackupTo}

	first, firstBackups := partition(fresh, decisions)
	// Map iteration order varies, so this must be sorted to be stable.
	for range 20 {
		got, gotBackups := partition(fresh, decisions)
		if !slices.Equal(got, first) || !slices.Equal(gotBackups, firstBackups) {
			t.Fatalf("partition varied between runs: %v / %v then %v / %v",
				first, firstBackups, got, gotBackups)
		}
	}
	if !slices.IsSorted(first) {
		t.Errorf("apply = %v, want it sorted", first)
	}
}

func TestPartitionWithNothingToDo(t *testing.T) {
	apply, backups := partition(nil, nil)
	if len(apply) != 0 || len(backups) != 0 {
		t.Errorf("partition(nil, nil) = %v / %v, want empty", apply, backups)
	}
}

func TestPartitionOnlyFreshFiles(t *testing.T) {
	apply, backups := partition([]string{".config/nvim"}, map[string]Resolution{})
	if !slices.Equal(apply, []string{".config/nvim"}) {
		t.Errorf("apply = %v", apply)
	}
	// Nothing existed, so nothing needs backing up.
	if len(backups) != 0 {
		t.Errorf("backups = %v, want empty", backups)
	}
}

// -- drift -------------------------------------------------------------------

func TestDriftClean(t *testing.T) {
	if !(drift{}).clean() {
		t.Error("an empty drift is not reported clean")
	}
	cases := map[string]drift{
		"missing pacman package": {pacman: []string{"fish"}},
		"missing AUR package":    {aur: []string{"quickshell"}},
		"new config":             {fresh: []string{".config/nvim"}},
		"conflicting config":     {conflicts: []string{".tmux.conf"}},
	}
	for name, d := range cases {
		if d.clean() {
			t.Errorf("drift with a %s is reported clean", name)
		}
	}
}

// -- presentation ------------------------------------------------------------

func TestCountLabel(t *testing.T) {
	if got := countLabel(0, "to install", "already installed"); !strings.Contains(got, "already installed") {
		t.Errorf("countLabel(0) = %q, want the none-text", got)
	}
	if got := countLabel(3, "to install", "already installed"); got != "3 to install" {
		t.Errorf("countLabel(3) = %q, want %q", got, "3 to install")
	}
	if got := countLabel(1, "step(s) to run", "nothing pending"); got != "1 step(s) to run" {
		t.Errorf("countLabel(1) = %q", got)
	}
}

func TestPlanSummary(t *testing.T) {
	t.Run("nothing to do", func(t *testing.T) {
		got := planSummary(nil, nil, nil, nil)
		if !strings.Contains(got, "Nothing to install") {
			t.Errorf("planSummary = %q, want it to say there is nothing to install", got)
		}
	})

	t.Run("everything at once", func(t *testing.T) {
		got := planSummary([]string{"fish", "tmux"}, []string{"quickshell"},
			[]string{".config/nvim", ".tmux.conf"}, []string{".tmux.conf"})

		for _, want := range []string{"3 package(s)", "2 config path(s)", "1 existing file(s)"} {
			if !strings.Contains(got, want) {
				t.Errorf("planSummary = %q, missing %q", got, want)
			}
		}
	})

	t.Run("packages only", func(t *testing.T) {
		got := planSummary([]string{"fish"}, nil, nil, nil)
		if !strings.Contains(got, "1 package(s)") {
			t.Errorf("planSummary = %q", got)
		}
		if strings.Contains(got, "config path") || strings.Contains(got, "back up") {
			t.Errorf("planSummary = %q, want no line for the empty categories", got)
		}
	})

	t.Run("no trailing newline", func(t *testing.T) {
		got := planSummary([]string{"fish"}, nil, []string{".x"}, nil)
		if strings.HasSuffix(got, "\n") {
			t.Errorf("planSummary = %q, want it trimmed", got)
		}
	})
}

func TestPendingSteps(t *testing.T) {
	done, notDone := func() bool { return true }, func() bool { return false }

	selected := []Component{
		{Key: "a", Post: []Step{
			{Name: "already done", Check: done, Run: func() error { return nil }},
			{Name: "still to do", Check: notDone, Run: func() error { return nil }},
		}},
		{Key: "b", Post: []Step{
			{Name: "no check at all", Run: func() error { return nil }},
		}},
		{Key: "c"},
	}

	got := pendingSteps(selected)
	want := []string{"still to do", "no check at all"}
	if !slices.Equal(got, want) {
		t.Errorf("pendingSteps = %v, want %v", got, want)
	}
}

func TestPendingStepsWithNothingSelected(t *testing.T) {
	if got := pendingSteps(nil); len(got) != 0 {
		t.Errorf("pendingSteps(nil) = %v, want empty", got)
	}
}

// -- resolution values -------------------------------------------------------

// The Resolution constants are written into the confirm prompt verbatim, so
// they need to read as words rather than as enum names.
func TestResolutionValues(t *testing.T) {
	cases := map[Resolution]string{
		Overwrite: "overwrite",
		BackupTo:  "backup",
		Keep:      "keep",
		ShowDiff:  "diff",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("resolution = %q, want %q", got, want)
		}
	}
}

func TestResolveConflictsWithNone(t *testing.T) {
	// No conflicts must return immediately rather than trying to draw a form,
	// which would block with no terminal attached.
	got, err := resolveConflicts(nil)
	if err != nil {
		t.Fatalf("resolveConflicts(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("decisions = %v, want empty", got)
	}
}
