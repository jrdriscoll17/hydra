# hydra

Keep several machines that are meant to be identical from drifting apart.

One config repo — a [chezmoi](https://chezmoi.io) source — describes the system.
hydra installs the packages it needs, hands the config files to chezmoi, runs the
bootstrap that nothing else tracks, and builds the GTK/icon/Kvantum theme assets.
Run it on a new machine to reproduce the setup; run it on an existing one to pull
in whatever changed elsewhere.

It exists because "I set up something nice on the desktop" kept meaning "now redo
90% of it on the laptop and the work box, and watch all three drift anyway".

```sh
go install github.com/jrdriscoll17/hydra@latest
~/go/bin/hydra init   # clone the config repo, set up chezmoi, link `theme`
~/go/bin/hydra        # choose components and install them
```

The full path is not a typo, and only these first two commands need it. `go
install` writes to `$(go env GOPATH)/bin`, which on a machine this has never run
on is not yet on `PATH` — the shell config that puts it there is one of the
files hydra is about to deploy. Start a new shell afterwards and `hydra`,
`theme` and everything else resolve by name.

Then, on any machine, whenever something changed on another:

```sh
hydra status          # what has drifted here; changes nothing
hydra sync            # pull the config repo and reapply
```

## Commands

| | |
|---|---|
| `hydra init [repo]` | Clone the config repo, `chezmoi init`, create the `theme` symlink. Defaults to the repo in `sync.go`. |
| `hydra` | Pick components, install packages, apply configs, run bootstrap. Records the selection. |
| `hydra status` | Report drift — missing packages, absent or differing config, leftovers the repo no longer has, pending bootstrap, failed system checks. Read-only. |
| `hydra sync` | Pull the config repo and reapply, using the components this machine already opted into. |
| `hydra recolor <base> <#hex> <name>` | Build a Material-Black + Suru-GLOW theme pair in an arbitrary accent. |
| `hydra theme <cmd>` | The theme switcher. Also reachable as `theme`. |

## How it decides what to do

Everything comes from the catalog in `internal/setup/catalog.go`: one entry per
component, listing its packages, the config paths it owns, and the post-install
steps that have to happen once the files land. Adding a component means adding an
entry there and nothing else.

Every action is checked before it runs, so a second run installs only what is
missing. The checks are about the machine's actual state, not a "have I run
before" marker: the theme render, for instance, compares a fingerprint of this
binary against the one that produced the output on disk, so upgrading hydra
makes `hydra status` report the render as pending and `hydra sync` redo it
without being asked.

Components are opt-out, and are filtered by host — laptop-only and desktop-only
entries appear based on whether a battery exists, not on a hostname.

When a config already exists and differs from the repo, hydra stops and asks, per
file: show the diff, keep yours, back yours up and take the repo's, or overwrite.
Answer once and it offers to apply the same choice to the rest. Backups land
beside the original as `<name>.before-setup`.

## Files the repo used to have

chezmoi writes the files its source describes and nothing else, so deleting a
file from the repo does not delete it from the machines that already applied it.
Usually that leaves dead weight. In a directory something loads wholesale it
leaves live config: a plugin spec dropped from the repo is still imported by
lazy.nvim, a QML file from an older layout is still resolvable by the bar, an old
`hyprland.conf` still sits next to the `hyprland.lua` that replaced it. The
machine keeps behaving like the version of the repo it had years ago, on that
machine alone, and every check says it is in sync.

So a catalog entry can mark directories `Exclusive`, meaning the repo owns every
file in them. Anything else found there is reported by `hydra status` and, on
`hydra sync`, offered up to be moved aside — renamed to `<name>.before-setup`,
never deleted.

Some drift cannot be seen in a file at all. Neovim is the case that prompted
this: `lua/theme.lua` names a colorscheme, but what the editor ends up on is
whatever called `colorscheme` last, so a leftover spec ends with the editor
ignoring theme switches while every other application follows them, silently.
The check for it starts nvim and asks. Anything short of a straight
contradiction passes — a check that fires when it cannot tell gets ignored, and
then so does the real one.

## Layout

```
main.go            argv dispatch
internal/sys       process, filesystem and text helpers
internal/theme     palette · renderers · icon index · config editing · CLI
internal/recolor   Material-Black + Suru-GLOW recolouring
internal/setup     catalog · detection · bootstrap steps · theme assets · checks · TUI
```

It is a multi-call binary: through a `theme` symlink `main()` dispatches to the
switcher instead of the installer, so both ship as one artefact. Quickshell execs
`theme data`, which is why that symlink has to be on `PATH`; `hydra init` creates
it.

## Tests

```sh
make test
```

Everything runs against a temporary `HOME` and a fixture palette, so the suite
never touches the machine it runs on and needs no Arch, no chezmoi and no
desktop. The parts that genuinely shell out — pacman, sudo, git clone, the
prompts — are the uncovered remainder.

Two things are worth knowing before changing code here:

- **The generated configs are golden files.** `internal/theme/testdata` holds
  the exact bytes each renderer produces for a fixture palette, because other
  programs parse those files and several are diffed against the config repo. A
  failing golden means a generated config changed shape. Look at the diff, then
  `make golden` to re-bless it.
- **The colour maths is pinned to Python.** The reference values in
  `internal/recolor` come from `colorsys` and `round()`, which is what
  `recolor.py` used, down to round-half-to-even. They are what keeps a rebuilt
  theme byte-identical to the one that came before it.

## Assumptions

This is a personal tool and does not pretend otherwise. It assumes Arch (pacman,
optionally paru/yay for AUR), and the catalog is one specific desktop: Hyprland,
Quickshell, kitty/alacritty, fish, neovim, Doom. The theme renderers target a
fixed set of apps and a palette schema defined in `internal/theme/palette.go`.

Fork it and rewrite `catalog.go` if you want a different system; everything else
— the chezmoi driving, conflict resolution, drift reporting, theme rendering — is
independent of which components you list.
