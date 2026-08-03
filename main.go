// Command hydra keeps several machines that are meant to be identical from
// drifting apart.
//
// One config repo (a chezmoi source) describes the system; hydra installs the
// packages it needs, hands the config files to chezmoi, runs the bootstrap that
// nothing else tracks, and rebuilds the theme assets. Run it on a new machine to
// reproduce the setup, and on an existing one to pull in whatever changed
// elsewhere.
//
//	hydra init [repo]   clone the config repo, set up chezmoi, link `theme`
//	hydra               choose components and install them
//	hydra status        what has drifted on this machine; changes nothing
//	hydra sync          pull the config repo and put this machine back in line
//	hydra monitors      record this machine's screen layout for Hyprland
//	hydra recolor …     build a Material-Black + Suru-GLOW pair
//	hydra theme …       the theme switcher
//
// It is also a multi-call binary: through a `theme` symlink it is the switcher,
// which is how Quickshell invokes it. `hydra init` creates that symlink.
//
// Everything is checked before it runs, so re-running installs only what is
// missing and asks only about files that actually differ.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"

	"github.com/jrdriscoll17/hydra/internal/recolor"
	"github.com/jrdriscoll17/hydra/internal/setup"
	"github.com/jrdriscoll17/hydra/internal/theme"
)

const usage = `hydra — keep your machines identical

  hydra init [repo]   clone the config repo, set up chezmoi, link ` + "`theme`" + `
  hydra               choose components and install them
  hydra status        what has drifted on this machine; changes nothing
  hydra sync          pull the config repo and reapply
  hydra monitors      record this machine's screen layout for Hyprland
  hydra recolor <base> <#hex> <name>
  hydra theme <cmd>   the theme switcher (also reachable as ` + "`theme`" + `)`

func main() {
	// argv[0] first: invoked through the `theme` symlink this is the switcher.
	if filepath.Base(os.Args[0]) == "theme" {
		exit("theme", theme.Main(os.Args[1:]))
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			exit("init", setup.Init(arg(2)))
		case "status":
			exit("status", setup.Status())
		case "sync":
			exit("sync", setup.Sync())
		case "monitors":
			exit("monitors", setup.Monitors())
		case "theme":
			exit("theme", theme.Main(os.Args[2:]))
		case "recolor":
			if len(os.Args) != 5 {
				fmt.Fprintln(os.Stderr, "usage: hydra recolor <base-variant> <#hex> <name>")
				os.Exit(2)
			}
			exit("recolor", recolor.Run(os.Args[2], os.Args[3], os.Args[4]))
		case "-h", "--help", "help":
			fmt.Println(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "hydra: unknown command %q\n\n%s\n", os.Args[1], usage)
			os.Exit(2)
		}
	}

	if err := setup.Run(); err != nil {
		// A cancelled prompt is a normal way to leave the TUI, not a failure.
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("\naborted — nothing was changed")
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "hydra: "+err.Error())
		os.Exit(1)
	}
}

// arg returns os.Args[i] or "" when it is absent.
func arg(i int) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return ""
}

// exit ends the process after a subcommand, reporting failures the way that
// command's users expect.
func exit(name string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
	os.Exit(0)
}
