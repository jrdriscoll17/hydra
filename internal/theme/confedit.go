package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/sys"
)

// Editing configs the switcher does not own outright. These files belong to
// other tools; only the theme-owned lines are retargeted, so hand-made settings
// elsewhere in them survive a switch.

func stripHash(c string) string { return strings.TrimPrefix(c, "#") }

// rgbaColor renders Hyprland's rgba(RRGGBBAA).
func rgbaColor(c string, a float64) string {
	return fmt.Sprintf("rgba(%s%02x)", stripHash(c), int(roundHalfUp(a*255)))
}

// roundHalfUp matches Python's round() for the values used here. Only ever
// called with 255 and 0.93*255 / 0.67*255, none of which land on .5.
func roundHalfUp(f float64) float64 {
	return float64(int(f + 0.5))
}

// subLine rewrites one setting in a config we do not own outright.
func subLine(path, pattern, replacement string) error {
	if !sys.Exists(path) {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	re, err := regexp.Compile("(?m)" + pattern)
	if err != nil {
		return err
	}
	text := string(raw)
	out := re.ReplaceAllLiteralString(text, replacement)
	if out == text {
		return nil
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// setINIKey sets key=value, creating the file, the section or the key as
// needed. subLine alone silently does nothing when the key is absent, which is
// how a config can end up stranded on the previous theme.
func setINIKey(path, section, key, value string) error {
	line := key + "=" + value
	var text string
	if raw, err := os.ReadFile(path); err == nil {
		text = string(raw)
	}

	keyRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=`)
	sectionRe := regexp.MustCompile(`(?m)^\[` + regexp.QuoteMeta(section) + `\]`)

	switch {
	case keyRe.MatchString(text):
		full := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=.*$`)
		text = full.ReplaceAllLiteralString(text, line)
	case sectionRe.MatchString(text):
		text = sys.ReplaceFirstFunc(sectionRe, text, func(m string) string {
			return m + "\n" + line
		})
	default:
		text = strings.TrimLeft(strings.TrimRight(text, " \t\n\r")+
			"\n\n["+section+"]\n"+line+"\n", " \t\n\r")
	}
	return sys.WriteFile(path, text)
}

// themeSearch finds an installed GTK theme directory.
func themeSearch(name string) string {
	for _, root := range []string{
		sys.InHome(".themes"), sys.InHome(".local/share/themes"), "/usr/share/themes",
	} {
		dir := filepath.Join(root, name)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
	}
	return ""
}
