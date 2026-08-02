package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jrdriscoll17/hydra/internal/recolor"
	"github.com/jrdriscoll17/hydra/internal/sys"
	"github.com/jrdriscoll17/hydra/internal/theme"
)

// Building the GTK and icon themes the palettes point at. These are the one
// part of the system nothing packages: the Material-Black + Suru-GLOW base is
// cloned from upstream, the Colloid gtk4 themes come from their installer, and
// each palette is then derived in its own accent.

// variantOf is the stem the recolour builds against: the palette's GTK theme
// "Material-Black-Evergreen" and icon set "MB-Evergreen-Suru-GLOW" share
// "Evergreen".
func variantOf(t *theme.Theme) string {
	return strings.TrimPrefix(t.GTK.Theme, "Material-Black-")
}

// assetsBuilt reports whether a palette's GTK theme and icon set are on disk.
func assetsBuilt(t *theme.Theme) bool {
	return sys.Exists(sys.InHome(".themes/"+t.GTK.Theme)) &&
		sys.Exists(sys.InHome(".local/share/icons/"+t.GTK.Icons))
}

// palettes reads every theme definition, skipping any that will not parse.
func palettes() []*theme.Theme {
	names, err := theme.Names()
	if err != nil {
		return nil
	}
	var out []*theme.Theme
	for _, n := range names {
		if t, err := theme.Load(n); err == nil {
			out = append(out, t)
		}
	}
	return out
}

// themeBaseInstalled reports whether any Material-Black + Suru-GLOW pair is
// present. The recolour can derive from any of them, including one of its own
// earlier outputs, so the upstream base does not have to be the one on disk.
func themeBaseInstalled() bool { return installedBase() != "" }

// installThemeBase sparse-clones just the one colour pair (~150M) rather than
// the whole ~850M repo, and drops the version suffix upstream uses so the
// directory name is the "Material-Black-<base>" that readBase looks for.
func installThemeBase() error {
	tmp, err := os.MkdirTemp("", "mbtheme")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := sys.Run("git", "clone", "--depth", "1", "--single-branch",
		"--branch", mbBranch, "--filter=blob:none", "--sparse", mbRepo, tmp); err != nil {
		return err
	}
	if err := sys.Run("git", "-C", tmp, "sparse-checkout", "set", mbTheme, mbIcons); err != nil {
		return err
	}

	for _, d := range []string{sys.InHome(".themes"), sys.InHome(".local/share/icons")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	base := strings.TrimSuffix(mbTheme, "-3.36")
	if err := sys.Run("cp", "-a", filepath.Join(tmp, mbTheme), sys.InHome(".themes/"+base)); err != nil {
		return err
	}
	return sys.Run("cp", "-a", filepath.Join(tmp, mbIcons), sys.InHome(".local/share/icons/"))
}

// colloidBuilt reports whether a Colloid theme is actually usable, which means
// the gtk4 sheet renderGTK symlinks is there. The directory alone is not
// enough: install.sh creates it before compiling, so a run without sassc leaves
// a complete-looking tree with no stylesheet in it, and renderGTK then silently
// links nothing.
func colloidBuilt(name string) bool {
	return sys.Exists(sys.InHome(".themes/" + name + "/gtk-4.0/gtk.css"))
}

// colloidVariants returns the gtk4 theme names the palettes ask for that are
// not usable yet.
func colloidVariants() []string {
	seen := map[string]bool{}
	var want []string
	for _, p := range palettes() {
		n := p.GTK.GTK4
		if n == "" || seen[n] || colloidBuilt(n) {
			continue
		}
		seen[n] = true
		want = append(want, n)
	}
	return want
}

func colloidInstalled() bool { return len(colloidVariants()) == 0 }

// Colloid's install.sh builds a directory name by concatenating four optional
// suffixes in a fixed order, each drawn from its own list, with the default
// member of every list contributing nothing:
//
//	Colloid [-Accent] [-Colour] [-Size] [-Scheme]
//
// So the segments have to be matched against those lists rather than read
// positionally — "Colloid-Dark" is a colour with the default accent, while
// "Colloid-Green" is an accent with the default colour.
var (
	colloidColours = []string{"light", "dark"}
	colloidSizes   = []string{"compact"}
	colloidSchemes = []string{"nord", "dracula", "gruvbox", "everforest", "catppuccin"}
)

// colloidFlags maps a theme directory name back to install.sh flags:
//
//	Colloid-Green-Dark-Everforest -> -t green -c dark --tweaks everforest
//	Colloid-Dark                  -> -c dark
//	Colloid-Green                 -> -t green -c standard
//
// Reports false only for a name that is not a Colloid theme at all.
func colloidFlags(name string) ([]string, bool) {
	parts := strings.Split(name, "-")
	if len(parts) < 2 || parts[0] != "Colloid" {
		return nil, false
	}

	accent, colour, size := "", "standard", ""
	var schemes []string
	for _, raw := range parts[1:] {
		seg := strings.ToLower(raw)
		switch {
		case slices.Contains(colloidColours, seg):
			colour = seg
		case slices.Contains(colloidSizes, seg):
			size = seg
		case slices.Contains(colloidSchemes, seg):
			schemes = append(schemes, seg)
		case accent == "":
			accent = seg
		}
	}

	var flags []string
	if accent != "" {
		flags = append(flags, "-t", accent)
	}
	flags = append(flags, "-c", colour)
	if size != "" {
		flags = append(flags, "-s", size)
	}
	for _, s := range schemes {
		flags = append(flags, "--tweaks", s)
	}
	return flags, true
}

func installColloid() error {
	variants := colloidVariants()
	if len(variants) == 0 {
		return nil
	}

	tmp, err := os.MkdirTemp("", "colloid")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := sys.Run("git", "clone", "--depth", "1", colloidRepo, tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(sys.InHome(".themes"), 0o755); err != nil {
		return err
	}

	for _, v := range variants {
		flags, ok := colloidFlags(v)
		if !ok {
			fmt.Printf("    skipping %s: cannot derive install flags from the name\n", v)
			continue
		}
		args := append([]string{
			filepath.Join(tmp, "install.sh"),
			"-d", sys.InHome(".themes"),
		}, flags...)
		if err := sys.Run("bash", args...); err != nil {
			return fmt.Errorf("installing %s: %w", v, err)
		}
	}
	return nil
}

func paletteThemesBuilt() bool {
	for _, p := range palettes() {
		if p.GTK.Theme != "" && !assetsBuilt(p) {
			return false
		}
	}
	return true
}

// buildPaletteThemes derives each palette's GTK theme and icon set from an
// installed base, in the palette's own accent colour. This is what turns the
// one upstream pair into the per-theme assets the switcher expects.
func buildPaletteThemes() error {
	base := installedBase()
	if base == "" {
		return errors.New("no Material-Black + Suru-GLOW pair to derive from")
	}
	for _, p := range palettes() {
		if p.GTK.Theme == "" || assetsBuilt(p) {
			continue
		}
		if p.Color("accent") == "" {
			fmt.Printf("    skipping %s: no accent colour in the palette\n", p.Name)
			continue
		}
		fmt.Printf("    %s (%s from %s)\n", variantOf(p), p.Color("accent"), base)
		if err := recolor.Run(base, p.Color("accent"), variantOf(p)); err != nil {
			return fmt.Errorf("recolouring %s: %w", variantOf(p), err)
		}
	}
	return nil
}

// installedBase returns the stem of any Material-Black + Suru-GLOW pair on
// disk, which the recolour can use as a source.
func installedBase() string {
	entries, err := os.ReadDir(sys.InHome(".themes"))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := strings.TrimPrefix(e.Name(), "Material-Black-")
		if name == e.Name() {
			continue
		}
		if sys.Exists(sys.InHome(".themes/"+e.Name()+"/gtk-3.0/gtk.css")) &&
			sys.Exists(sys.InHome(".local/share/icons/MB-"+name+"-Suru-GLOW/places/scalable/folder.svg")) {
			return name
		}
	}
	return ""
}
