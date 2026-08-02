package sys

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func TestHomeAndInHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if got := Home(); got != dir {
		t.Errorf("Home() = %q, want %q", got, dir)
	}
	want := filepath.Join(dir, ".config", "kitty")
	if got := InHome(".config/kitty"); got != want {
		t.Errorf("InHome() = %q, want %q", got, want)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"file", file, true},
		{"directory", dir, true},
		// Exists uses Lstat deliberately: a dangling link still occupies the
		// path, so callers that are about to create something there must see it.
		{"dangling symlink", dangling, true},
		{"missing", filepath.Join(dir, "nope"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Exists(c.path); got != c.want {
				t.Errorf("Exists(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestHave(t *testing.T) {
	if !Have("sh") {
		t.Error("Have(sh) = false, want true")
	}
	if Have("hydra-definitely-not-a-real-binary") {
		t.Error("Have(nonexistent) = true, want false")
	}
}

func TestCapture(t *testing.T) {
	out, err := Capture("printf", "  hello  ")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if out != "hello" {
		t.Errorf("Capture = %q, want %q (output should be trimmed)", out, "hello")
	}

	if _, err := Capture("sh", "-c", "exit 3"); err == nil {
		t.Error("Capture of a failing command returned nil error")
	}
	if _, err := Capture("hydra-definitely-not-a-real-binary"); err == nil {
		t.Error("Capture of a missing binary returned nil error")
	}
}

func TestQuiet(t *testing.T) {
	t.Run("missing binary", func(t *testing.T) {
		out, ok := Quiet("hydra-definitely-not-a-real-binary")
		if ok || out != "" {
			t.Errorf("Quiet(missing) = (%q, %v), want (\"\", false)", out, ok)
		}
	})

	t.Run("success", func(t *testing.T) {
		out, ok := Quiet("printf", "hi")
		if !ok || out != "hi" {
			t.Errorf("Quiet = (%q, %v), want (\"hi\", true)", out, ok)
		}
	})

	// The hyprctl callers depend on this: `hyprctl hyprpaper listactive` exits
	// non-zero in situations where its stdout is still the answer.
	t.Run("stdout survives a non-zero exit", func(t *testing.T) {
		out, ok := Quiet("sh", "-c", "printf partial; exit 1")
		if ok {
			t.Error("Quiet reported success for a failing command")
		}
		if out != "partial" {
			t.Errorf("Quiet dropped stdout on failure: got %q, want %q", out, "partial")
		}
	})
}

func TestWriteFileCreatesParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c.conf")

	if err := WriteFile(path, "content\n"); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(raw) != "content\n" {
		t.Errorf("content = %q, want %q", raw, "content\n")
	}

	if err := WriteFile(path, "replaced\n"); err != nil {
		t.Fatalf("WriteFile (overwrite): %v", err)
	}
	raw, _ = os.ReadFile(path)
	if string(raw) != "replaced\n" {
		t.Errorf("overwrite left %q, want %q", raw, "replaced\n")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := CopyFile(src, dst, 0o755); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	raw, _ := os.ReadFile(dst)
	if string(raw) != "payload" {
		t.Errorf("content = %q, want %q", raw, "payload")
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755 (the requested mode, not the source's)", fi.Mode().Perm())
	}

	if err := CopyFile(filepath.Join(dir, "missing"), dst, 0o644); err == nil {
		t.Error("CopyFile from a missing source returned nil error")
	}
}

func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	mkdirAll(t, filepath.Join(src, "places", "scalable"))
	writeFile(t, filepath.Join(src, "places", "scalable", "folder.svg"), "<svg/>", 0o644)
	writeFile(t, filepath.Join(src, "exec.sh"), "#!/bin/sh\n", 0o755)

	// The icon sets are mostly symlinks; following them would multiply ~25k
	// icons into a far larger tree, so they must survive as links.
	if err := os.Symlink("folder.svg", filepath.Join(src, "places", "scalable", "dir.svg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nowhere/at/all", filepath.Join(src, "broken")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dst, "places", "scalable", "folder.svg"))
	if err != nil || string(raw) != "<svg/>" {
		t.Errorf("regular file not copied: %q, %v", raw, err)
	}

	link, err := os.Readlink(filepath.Join(dst, "places", "scalable", "dir.svg"))
	if err != nil {
		t.Fatalf("symlink not preserved: %v", err)
	}
	if link != "folder.svg" {
		t.Errorf("symlink target = %q, want %q", link, "folder.svg")
	}

	// A dangling link is still a link; copying must not try to read through it.
	if link, err := os.Readlink(filepath.Join(dst, "broken")); err != nil || link != "/nowhere/at/all" {
		t.Errorf("dangling symlink = (%q, %v), want (/nowhere/at/all, nil)", link, err)
	}

	fi, err := os.Stat(filepath.Join(dst, "exec.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755 preserved", fi.Mode().Perm())
	}
}

func TestRewrite(t *testing.T) {
	t.Run("writes when changed, preserving mode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		writeFile(t, path, "old", 0o600)

		err := Rewrite(path, func(s string) (string, bool) { return "new", true })
		if err != nil {
			t.Fatalf("Rewrite: %v", err)
		}
		raw, _ := os.ReadFile(path)
		if string(raw) != "new" {
			t.Errorf("content = %q, want %q", raw, "new")
		}
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600 preserved", fi.Mode().Perm())
		}
	})

	// Not writing at all is the point: the theme renderers walk ~25k icon files
	// and most match nothing.
	t.Run("does not write when unchanged", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		writeFile(t, path, "same", 0o644)
		before, _ := os.Stat(path)

		err := Rewrite(path, func(s string) (string, bool) { return "ignored", false })
		if err != nil {
			t.Fatalf("Rewrite: %v", err)
		}
		raw, _ := os.ReadFile(path)
		if string(raw) != "same" {
			t.Errorf("content = %q, want it untouched", raw)
		}
		after, _ := os.Stat(path)
		if !before.ModTime().Equal(after.ModTime()) {
			t.Error("file was rewritten despite the callback reporting no change")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		err := Rewrite(filepath.Join(t.TempDir(), "nope"), func(s string) (string, bool) {
			t.Error("callback ran on a missing file")
			return s, false
		})
		if err == nil {
			t.Error("Rewrite on a missing file returned nil error")
		}
	})

	t.Run("callback sees the current contents", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		writeFile(t, path, "abc", 0o644)
		var seen string
		_ = Rewrite(path, func(s string) (string, bool) {
			seen = s
			return s + "d", true
		})
		if seen != "abc" {
			t.Errorf("callback saw %q, want %q", seen, "abc")
		}
		raw, _ := os.ReadFile(path)
		if string(raw) != "abcd" {
			t.Errorf("content = %q, want %q", raw, "abcd")
		}
	})
}

func TestReplaceFirst(t *testing.T) {
	re := regexp.MustCompile(`#[0-9a-f]{6}`)

	cases := []struct {
		name, in, want string
	}{
		{"only the first match", "a #aabbcc b #ddeeff", "a #000000 b #ddeeff"},
		{"no match is a no-op", "nothing here", "nothing here"},
		{"match at the very start", "#aabbcc tail", "#000000 tail"},
		{"match at the very end", "head #aabbcc", "head #000000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ReplaceFirst(re, c.in, "#000000"); got != c.want {
				t.Errorf("ReplaceFirst = %q, want %q", got, c.want)
			}
		})
	}
}

func TestReplaceFirstFunc(t *testing.T) {
	// This is how the icon index.theme gets an Inherits line appended after
	// Comment=, so the callback must receive the matched text itself.
	re := regexp.MustCompile(`(?m)^Comment=.*$`)
	in := "Name=X\nComment=hello\nComment=second\n"
	want := "Name=X\nComment=hello\nInherits=Y\nComment=second\n"

	got := ReplaceFirstFunc(re, in, func(m string) string { return m + "\nInherits=Y" })
	if got != want {
		t.Errorf("ReplaceFirstFunc = %q, want %q", got, want)
	}

	calls := 0
	ReplaceFirstFunc(regexp.MustCompile("zzz"), "abc", func(m string) string {
		calls++
		return m
	})
	if calls != 0 {
		t.Errorf("callback ran %d times with no match, want 0", calls)
	}
}

func TestSubdirs(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"scalable", "apps", "16x16"} {
		mkdirAll(t, filepath.Join(dir, d))
	}
	writeFile(t, filepath.Join(dir, "index.theme"), "x", 0o644)

	// Subdirs stats rather than using the dirent type, so a symlink pointing at
	// a directory counts as one — which is what the icon themes rely on.
	if err := os.Symlink(filepath.Join(dir, "apps"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}

	got := Subdirs(dir)
	want := []string{"16x16", "apps", "linked", "scalable"}
	if !slices.Equal(got, want) {
		t.Errorf("Subdirs = %v, want %v (sorted, dirs only, links to dirs included)", got, want)
	}

	if got := Subdirs(filepath.Join(dir, "does-not-exist")); got != nil {
		t.Errorf("Subdirs(missing) = %v, want nil", got)
	}
}

// -- helpers -----------------------------------------------------------------

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
