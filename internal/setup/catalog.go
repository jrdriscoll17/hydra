package setup

// Component is one selectable unit of the setup: the packages it needs, the
// config paths it owns, and any bootstrap that has to happen after the files
// land. Everything the TUI offers comes from this catalog, so adding a new
// piece of the system means adding an entry here and nothing else.
type Component struct {
	Key     string
	Name    string
	Desc    string
	Default bool

	// Host filters. A component with DesktopOnly is hidden on a laptop and
	// vice versa; both false means it applies everywhere.
	DesktopOnly bool
	LaptopOnly  bool

	// Packages resolved through pacman; AUR through the detected helper.
	Packages []string
	AUR      []string

	// Config paths this component owns, as chezmoi target paths relative to
	// $HOME. These are what get applied (and what conflicts are reported on).
	Paths []string

	// Exclusive names the directories, among Paths, where the repo owns every
	// file — so anything else found in one is a leftover from an older version
	// of the repo rather than something the machine is entitled to have. Set it
	// for directories that are loaded wholesale, where a stale file is not
	// clutter but config that still runs. See stray.go.
	Exclusive []string

	// Post-install bootstrap, run once the config is in place.
	Post []Step
}

// Step is a named post-install action. Check reports whether it has already
// been done, so a re-run does not redo work or clobber an existing install.
type Step struct {
	Name  string
	Check func() bool
	Run   func() error
}

func catalog() []Component {
	return []Component{
		{
			Key:     "core",
			Name:    "Core shell",
			Desc:    "fish, tmux, git, fzf — the portable base",
			Default: true,
			Packages: []string{
				"fish", "tmux", "git", "fzf", "ripgrep", "fd", "less", "wl-clipboard",
				// config.fish aliases these. They are present on the desktop
				// because CachyOS ships them, so their absence only showed up on
				// a machine built from scratch — where `ls` is aliased to eza
				// and simply stopped working.
				"eza", "expac", "hwinfo", "wget",
			},
			Paths: []string{".config/fish", ".tmux.conf", ".config/git", ".gitconfig"},
			Post: []Step{
				{Name: "tmux plugin manager (tpm)", Check: tpmInstalled, Run: installTPM},
				{Name: "fish plugins (fisher)", Check: fisherInstalled, Run: installFisher},
			},
		},
		{
			Key:      "terminal",
			Name:     "Terminals",
			Desc:     "kitty + alacritty",
			Default:  true,
			Packages: []string{"kitty", "alacritty"},
			Paths:    []string{".config/kitty", ".config/alacritty"},
		},
		{
			Key:         "hyprland",
			Name:        "Hyprland",
			Desc:        "compositor, idle/lock/wallpaper daemons, portals, polkit agent",
			Default:     true,
			DesktopOnly: false,
			Packages: []string{
				"hyprland", "hypridle", "hyprlock", "hyprpaper",
				"xdg-desktop-portal-hyprland", "polkit-kde-agent",
				"udiskie", "playerctl", "wireplumber", "qt5-wayland", "qt6-wayland",
			},
			Paths: []string{".config/hypr"},
			// Hyprland picks its config out of this directory by name, and the
			// repo moved from hyprland.conf to hyprland.lua. A machine that
			// applied the old one still has it sitting next to the new one,
			// which leaves which config is live up to the compositor's search
			// order rather than to the repo.
			Exclusive: []string{".config/hypr"},
		},
		{
			Key:     "quickshell",
			Name:    "Quickshell bar",
			Desc:    "the hand-written bar/launcher (AUR)",
			Default: true,
			AUR:     []string{"quickshell"},
			Paths:   []string{".config/quickshell"},
			// shell.qml resolves its imports against this directory, so QML
			// files from the flat pre-refactor layout are still loadable from
			// where they sit. The generated palette is .chezmoiignore'd and so
			// is not reported here.
			Exclusive: []string{".config/quickshell"},
		},
		{
			// Ahead of the theme component deliberately: `theme apply` writes
			// hyprpaper.conf pointing into ~/.config/hypr/wallpapers, so that
			// link has to exist first or the first render names a file that is
			// not there and the desktop comes up with no wallpaper.
			Key:     "wallpapers",
			Name:    "Wallpapers",
			Desc:    "clone the wallpapers repo and link it where the switcher looks",
			Default: true,
			Post: []Step{
				{Name: "clone + link wallpapers", Check: wallpapersLinked, Run: linkWallpapers},
			},
		},
		{
			Key:     "theme",
			Name:    "Theme switcher",
			Desc:    "GTK/Qt/Kvantum theming — required by kitty's include",
			Default: true,
			Packages: []string{
				"glib2", "kvantum", "qt5ct", "qt6ct", "gtk3", "gtk4",
				// Colloid's install.sh compiles its stylesheets with sassc.
				// Without it the theme directory is still created but every
				// gtk.css is missing, which reads as "installed" and styles
				// nothing. Its other listed requirements are GTK2-only
				// (murrine, gnome-themes-extra) and hydra takes only the gtk4
				// sheet from Colloid, so they are deliberately not pulled in.
				"sassc",
			},
			// kvantumBase points at this theme's SVG, which renderQt copies
			// per palette; render_qt silently no-ops without it.
			AUR: []string{"kvantum-theme-nordic-git"},
			Paths: []string{
				".config/theme",
				".config/gtk-3.0", ".config/gtk-4.0", ".gtkrc-2.0",
				".config/qt5ct", ".config/qt6ct", ".config/mimeapps.list",
			},
			// Order matters: the palettes point at GTK and icon themes that
			// have to exist on disk before rendering means anything.
			Post: []Step{
				{Name: "base Material-Black + Suru-GLOW pair", Check: themeBaseInstalled, Run: installThemeBase},
				{Name: "Colloid gtk4 themes", Check: colloidInstalled, Run: installColloid},
				{Name: "derive per-palette GTK/icon themes", Check: paletteThemesBuilt, Run: buildPaletteThemes},
				{Name: "render the active theme", Check: themeRendered, Run: applyTheme},
			},
		},
		{
			// After the theme component, because lua/plugins/colorscheme.lua
			// does `require("theme")` on the generated lua/theme.lua. Sync the
			// plugins before that file exists and the whole spec fails to load,
			// so the colorschemes never install — while lazy itself does, which
			// is what made the step look done.
			Key:      "nvim",
			Name:     "Neovim",
			Desc:     "neovim + lazy.nvim plugin sync",
			Default:  true,
			Packages: []string{"neovim", "tree-sitter-cli"},
			Paths:    []string{".config/nvim"},
			// The directory nvim reads is entirely the repo's: lazy.nvim
			// imports every file under lua/plugins, and plugin/ and after/ are
			// sourced on sight. Nothing here is state — that lives under
			// ~/.local/{share,state}/nvim — so a file the repo does not have is
			// config from a previous version of it, still running.
			Exclusive: []string{".config/nvim"},
			Post: []Step{
				{Name: "sync lazy.nvim plugins", Check: lazySynced, Run: syncLazy},
			},
		},
		{
			// Likewise: config.el loads the generated theme.el.
			Key:      "emacs",
			Name:     "Doom Emacs",
			Desc:     "emacs + Doom installer — skip if you don't want it",
			Default:  false,
			Packages: []string{"emacs", "cmake", "libtool"},
			Paths:    []string{".config/doom"},
			Post: []Step{
				{Name: "install Doom Emacs", Check: doomInstalled, Run: installDoom},
			},
		},
		{
			Key:         "ddc",
			Name:        "DDC monitor control",
			Desc:        "ddcutil for the hypridle blank/wake scripts (needs i2c-dev)",
			Default:     true,
			DesktopOnly: true,
			Packages:    []string{"ddcutil"},
		},
		{
			Key:        "laptop",
			Name:       "Laptop extras",
			Desc:       "backlight + battery control",
			Default:    true,
			LaptopOnly: true,
			Packages:   []string{"brightnessctl", "upower", "power-profiles-daemon"},
		},
		{
			Key:      "btop",
			Name:     "btop",
			Desc:     "system monitor (themed by the switcher)",
			Default:  true,
			Packages: []string{"btop"},
			Paths:    []string{".config/btop"},
		},
		{
			Key:      "media",
			Name:     "Media",
			Desc:     "mpv + plex-mpv-shim",
			Default:  false,
			Packages: []string{"mpv"},
			AUR:      []string{"plex-mpv-shim"},
		},
	}
}

// defaults returns the components selected out of the box, which is what
// -plan reports on.
func defaults(available []Component) []Component {
	var out []Component
	for _, c := range available {
		if c.Default {
			out = append(out, c)
		}
	}
	return out
}

// forHost drops components that do not apply to this machine.
func forHost(all []Component, laptop bool) []Component {
	var out []Component
	for _, c := range all {
		if c.DesktopOnly && laptop {
			continue
		}
		if c.LaptopOnly && !laptop {
			continue
		}
		out = append(out, c)
	}
	return out
}
