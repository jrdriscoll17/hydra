package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProc builds a /proc with one process in it. inodes are the ones that
// process holds inotify watches on, spread over a few fds the way a real
// process does — the watches are never all on one fd.
func fakeProc(t *testing.T, pid, comm string, inodes []uint64) string {
	t.Helper()
	root := t.TempDir()
	procRoot = func() string { return root }
	t.Cleanup(func() { procRoot = func() string { return "/proc" } })

	dir := filepath.Join(root, pid)
	writeFile(t, filepath.Join(dir, "comm"), comm+"\n")

	// A non-inotify fd, so the parser has to ignore something.
	writeFile(t, filepath.Join(dir, "fdinfo", "3"), "pos:\t0\nflags:\t02\nmnt_id:\t24\n")

	for i, ino := range inodes {
		writeFile(t, filepath.Join(dir, "fdinfo", fmt.Sprint(10+i)),
			"pos:\t0\nflags:\t02000002\nmnt_id:\t15\n"+
				fmt.Sprintf("inotify wd:1 ino:%x sdev:1d mask:7c4 ignored_mask:0 "+
					"fhandle-bytes:14 fhandle-type:4d f_handle:deadbeef\n", ino))
	}

	// Directories that are not pids must be ignored.
	writeFile(t, filepath.Join(root, "self", "comm"), "qs\n")
	writeFile(t, filepath.Join(root, "meminfo"), "MemTotal: 1 kB\n")
	return root
}

// configInode returns the inode of a real ~/.config/quickshell created under a
// temp HOME. The check compares against the live directory, so the test has to
// use whatever inode the filesystem hands out rather than inventing one.
func configInode(t *testing.T, home string) uint64 {
	t.Helper()
	mkdir(t, filepath.Join(home, qsConfigDir))
	ino, ok := inodeOf(filepath.Join(home, qsConfigDir))
	if !ok {
		t.Fatal("could not stat the config dir just created")
	}
	return ino
}

func TestParseInotifyInode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want uint64
		ok   bool
	}{
		{
			name: "a real line",
			in:   "inotify wd:9 ino:10c sdev:1d mask:7c4 ignored_mask:0 fhandle-bytes:14",
			want: 0x10c, ok: true,
		},
		{
			name: "large inode",
			in:   "inotify wd:2 ino:75e370 sdev:1d mask:7c4",
			want: 0x75e370, ok: true,
		},
		{name: "not an inotify line", in: "pos:\t0", ok: false},
		{name: "empty", in: "", ok: false},
		// "inotify" as a prefix of something else must not match.
		{name: "lookalike", in: "inotifyish wd:1 ino:5", ok: false},
		{name: "no ino field", in: "inotify wd:1 sdev:1d", ok: false},
		{name: "unparseable ino", in: "inotify wd:1 ino:zzz sdev:1d", ok: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseInotifyInode(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok && got != c.want {
				t.Errorf("ino = %#x, want %#x", got, c.want)
			}
		})
	}
}

func TestQuickshellPIDs(t *testing.T) {
	fakeProc(t, "1234", "qs", nil)
	got := quickshellPIDs()
	if len(got) != 1 || got[0] != "1234" {
		t.Errorf("quickshellPIDs = %v, want [1234]", got)
	}
}

func TestQuickshellPIDsIgnoresOtherProcesses(t *testing.T) {
	root := fakeProc(t, "1234", "kitty", nil)
	if got := quickshellPIDs(); len(got) != 0 {
		t.Errorf("quickshellPIDs = %v, want none", got)
	}
	// The binary is `qs`; a process merely mentioning it is not the shell.
	writeFile(t, filepath.Join(root, "2222", "comm"), "qsv\n")
	if got := quickshellPIDs(); len(got) != 0 {
		t.Errorf("quickshellPIDs = %v, want none for a lookalike comm", got)
	}
}

// The healthy case: the watch set contains the config directory's inode.
func TestQuickshellWatchHealthy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ino := configInode(t, home)
	fakeProc(t, "1234", "qs", []uint64{0x10c, ino, 0x75e370})

	if got := quickshellWatch(); got != qsWatching {
		t.Errorf("quickshellWatch = %v, want qsWatching", got)
	}
	if c := quickshellWatchCheck(); !c.OK {
		t.Error("check is not OK with the config dir watched")
	}
}

// The failure this exists for: the shell is running and holds watches, but
// every one of them is on an inode that is no longer the config directory —
// what the stow→chezmoi migration did on 2026-08-02.
func TestQuickshellWatchBlindAfterInodesReplaced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configInode(t, home)
	// Watches on unrelated inodes: the old, now-unlinked tree.
	fakeProc(t, "1234", "qs", []uint64{0x100, 0x101, 0x102})

	if got := quickshellWatch(); got != qsBlind {
		t.Errorf("quickshellWatch = %v, want qsBlind", got)
	}
	c := quickshellWatchCheck()
	if c.OK {
		t.Error("check is OK while the shell cannot see its config")
	}
	if !strings.Contains(c.Fix, "qs kill") {
		t.Errorf("the fix should say how to restart it, got %q", c.Fix)
	}
}

// A shell holding no watches at all is just as blind as one holding the wrong
// ones, and is what a process whose fdinfo cannot be read looks like too.
func TestQuickshellWatchBlindWithNoWatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configInode(t, home)
	fakeProc(t, "1234", "qs", nil)

	if got := quickshellWatch(); got != qsBlind {
		t.Errorf("quickshellWatch = %v, want qsBlind", got)
	}
}

// Not running is not a problem. Sync runs on machines with no display session
// at all, and reporting a missing shell as drift would cry wolf on every one.
func TestQuickshellWatchNotRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configInode(t, home)
	fakeProc(t, "1234", "kitty", nil)

	if got := quickshellWatch(); got != qsNotRunning {
		t.Errorf("quickshellWatch = %v, want qsNotRunning", got)
	}
	if c := quickshellWatchCheck(); !c.OK {
		t.Error("check is not OK with no shell running")
	}
}

// No config directory means nothing to watch and nothing to report.
func TestQuickshellWatchNoConfigDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeProc(t, "1234", "qs", []uint64{0x100})

	if got := quickshellWatch(); got != qsNotRunning {
		t.Errorf("quickshellWatch = %v, want qsNotRunning", got)
	}
}

// Several shells can be running — a stale one and a fresh one during a restart.
// Any of them watching the config means a switch will be seen.
func TestQuickshellWatchAcrossSeveralProcesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ino := configInode(t, home)
	root := fakeProc(t, "1111", "qs", []uint64{0x100})
	writeFile(t, filepath.Join(root, "2222", "comm"), "qs\n")
	writeFile(t, filepath.Join(root, "2222", "fdinfo", "10"),
		fmt.Sprintf("inotify wd:1 ino:%x sdev:1d mask:7c4\n", ino))

	if got := quickshellWatch(); got != qsWatching {
		t.Errorf("quickshellWatch = %v, want qsWatching", got)
	}
}

// A missing /proc must not panic or claim the shell is blind.
func TestQuickshellWatchWithNoProc(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	procRoot = func() string { return filepath.Join(t.TempDir(), "nope") }
	t.Cleanup(func() { procRoot = func() string { return "/proc" } })

	if got := quickshellWatch(); got != qsNotRunning {
		t.Errorf("quickshellWatch = %v, want qsNotRunning", got)
	}
}

func TestWatchedInodesSkipsUnreadableFdinfo(t *testing.T) {
	root := fakeProc(t, "1234", "qs", []uint64{0x10c})
	// A directory where a file is expected: ReadFile fails, and the rest of the
	// fds must still be read.
	if err := os.MkdirAll(filepath.Join(root, "1234", "fdinfo", "99"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := watchedInodes("1234")
	if !got[0x10c] {
		t.Errorf("watchedInodes = %v, want the readable watch to survive", got)
	}
}
