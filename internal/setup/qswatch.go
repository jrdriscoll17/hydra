package setup

// Whether the running Quickshell can still see its own config.
//
// Quickshell hot-reloads by putting an inotify watch on each directory and QML
// file of its config at startup. A watch follows the *inode*, not the path, so
// anything that replaces a file rather than editing it in place leaves the
// watch pointing at something unlinked. The shell keeps running, keeps
// rendering, and never notices another config change.
//
// hypr-cachy lost five days to exactly this. The stow→chezmoi migration on
// 2026-08-02 replaced every symlink under ~/.config/quickshell with a real file
// and gave the directory a new inode, while `qs` had been running since the day
// before. `theme set` went on rewriting generated/Colors.qml correctly and the
// bar went on showing the old palette. Every other app updated, because nothing
// else depends on a long-lived watch — terminals re-read per window, GTK and Qt
// at startup, nvim at launch. There was no error anywhere.
//
// So this checks the thing that actually matters — is the config directory's
// current inode among the inotify watches the process holds — rather than
// whether the shell is running, which it was, perfectly, the whole time.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// procRoot is a var so the tests can build a fake /proc.
var procRoot = func() string { return "/proc" }

// qsConfigDir is the directory Quickshell loads and watches.
const qsConfigDir = ".config/quickshell"

// parseInotifyInode pulls the watched inode out of one fdinfo line.
//
// fdinfo is a fixed `key:value` format, so fields are split rather than matched
// with a regexp. A line looks like:
//
//	inotify wd:9 ino:10c sdev:1d mask:7c4 ignored_mask:0 fhandle-bytes:14 …
//
// ino is hex, and is the only field this needs.
func parseInotifyInode(line string) (uint64, bool) {
	if !strings.HasPrefix(line, "inotify ") {
		return 0, false
	}
	for _, f := range strings.Fields(line) {
		hex, ok := strings.CutPrefix(f, "ino:")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(hex, 16, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// watchedInodes reads every inode a process holds an inotify watch on.
//
// Unreadable fdinfo entries are skipped rather than failed on: fds come and go
// while this is reading, and a process owned by another user is not something
// to report on.
func watchedInodes(pid string) map[uint64]bool {
	dir := filepath.Join(procRoot(), pid, "fdinfo")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[uint64]bool{}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if ino, ok := parseInotifyInode(line); ok {
				out[ino] = true
			}
		}
	}
	return out
}

// quickshellPIDs finds the running shell.
//
// Matched on /proc/<pid>/comm rather than by shelling out to pgrep: the binary
// is `qs`, which is short enough that a command-line match would catch
// unrelated things.
func quickshellPIDs() []string {
	entries, err := os.ReadDir(procRoot())
	if err != nil {
		return nil
	}
	var pids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(procRoot(), e.Name(), "comm"))
		if err != nil {
			continue
		}
		switch strings.TrimSpace(string(raw)) {
		case "qs", "quickshell":
			pids = append(pids, e.Name())
		}
	}
	return pids
}

// inodeOf returns a path's inode. Comparing inodes rather than paths is the
// whole point: the path still resolves, which is why nothing else notices.
func inodeOf(path string) (uint64, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Ino, true
}

// quickshellWatchState is the three answers this can give, kept apart because
// "not running" is not a problem and "running but blind" very much is.
type quickshellWatchState int

const (
	qsNotRunning quickshellWatchState = iota
	qsWatching
	qsBlind
)

func quickshellWatch() quickshellWatchState {
	pids := quickshellPIDs()
	if len(pids) == 0 {
		return qsNotRunning
	}
	ino, ok := inodeOf(sys.InHome(qsConfigDir))
	if !ok {
		// No config directory at all: nothing to watch, and the theme switcher
		// has nothing to write into either. Not this check's problem.
		return qsNotRunning
	}
	for _, pid := range pids {
		if watchedInodes(pid)[ino] {
			return qsWatching
		}
	}
	return qsBlind
}

// quickshellWatchCheck reports a shell that has gone blind to its config.
//
// A shell that is not running is reported as fine. This runs during sync, which
// is often the moment before the desktop exists at all, and "Quickshell is not
// running" is not a drift from the repo — it is a fact about the session.
func quickshellWatchCheck() Check {
	state := quickshellWatch()
	return Check{
		Name: "quickshell is watching its config",
		OK:   state != qsBlind,
		Fix: fmt.Sprintf("`qs` is running but holds no watch on ~/%s — it will not "+
			"pick up a theme switch or any other config change until it is "+
			"restarted: qs kill; setsid -f qs -d", qsConfigDir),
	}
}
