// Package sys holds the small process, filesystem and text helpers the other
// packages share. It deliberately knows nothing about themes, packages or
// chezmoi — everything here would be at home in any tool.
package sys

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Home is the user's home directory, falling back to $HOME if the lookup fails.
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return h
}

// InHome joins a $HOME-relative path.
func InHome(rel string) string { return filepath.Join(Home(), rel) }

// Exists reports whether a path exists, without following symlinks — a dangling
// link still counts, which is what the callers here want.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Run executes a command with the user's terminal attached, so sudo can prompt
// and pacman can draw its progress bars.
func Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Capture runs a command silently and returns its trimmed stdout.
//
// A failure carries the command's stderr, because that is where the reason
// lives — an unadorned "exit status 1" tells the user nothing about what went
// wrong inside a tool hydra only drives.
func Capture(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("%w: %s", err, msg)
		}
	}
	return out, err
}

// CaptureWithin is Capture with a deadline.
//
// For commands hydra only asks a question of, where a slow answer is no more
// use than no answer. Starting an editor or a shell runs the user's whole
// config, and a config can sit at a prompt forever — waiting on that would hang
// a status report that is supposed to be read-only and quick.
func CaptureWithin(limit time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if ctx.Err() != nil {
		return out, fmt.Errorf("%s did not answer within %s", name, limit)
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("%w: %s", err, msg)
		}
	}
	return out, err
}

// Quiet is a best-effort side effect. A missing binary or a dead daemon is not
// fatal: the file on disk is already correct, so the next start picks it up.
// Unlike Capture it returns stdout even on a non-zero exit, which the hyprctl
// callers rely on.
func Quiet(name string, args ...string) (string, bool) {
	if !Have(name) {
		return "", false
	}
	cmd := exec.Command(name, args...)
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return out.String(), false
	}
	return out.String(), true
}

// Have reports whether a binary is on PATH.
func Have(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// WriteFile writes content, creating parent directories as needed.
func WriteFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// CopyFile copies one file, with the given mode.
func CopyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// CopyTree copies a directory verbatim, preserving symlinks and modes. The icon
// sets are mostly symlinks, so following them would multiply ~25k icons into a
// far larger tree.
func CopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		default:
			return CopyFile(path, target, info.Mode().Perm())
		}
	})
}

// Rewrite applies fn to a file's contents, writing back only if it changed.
func Rewrite(path string, fn func(string) (string, bool)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text, changed := fn(string(raw))
	if !changed {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), info.Mode().Perm())
}

// ReplaceFirst substitutes only the first match, which is what Python's
// re.sub(..., count=1) does.
func ReplaceFirst(re *regexp.Regexp, text, replacement string) string {
	return ReplaceFirstFunc(re, text, func(string) string { return replacement })
}

// ReplaceFirstFunc substitutes the first match with fn applied to it.
func ReplaceFirstFunc(re *regexp.Regexp, text string, fn func(string) string) string {
	loc := re.FindStringIndex(text)
	if loc == nil {
		return text
	}
	return text[:loc[0]] + fn(text[loc[0]:loc[1]]) + text[loc[1]:]
}

// Subdirs lists a directory's subdirectories by name, sorted, ignoring errors.
func Subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if fi, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && fi.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
