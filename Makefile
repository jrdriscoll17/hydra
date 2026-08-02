PREFIX ?= $(HOME)/.local/bin

.PHONY: build install plan test cover golden clean

build:
	go build -o hydra .

# The tests build their own world under a temporary HOME and never touch this
# machine, so they are safe to run anywhere.
test:
	go test ./...

cover:
	go test -cover ./...

# Re-bless the generated-config golden files after a deliberate renderer change.
# Read the diff first: these files are parsed by other programs.
golden:
	go test ./internal/theme -update

# Where the bootstrap one-liner points; ~/.local/bin is already on PATH.
#
# `theme` is the same binary under another name — main() dispatches on argv[0],
# so the switcher and the installer ship as one artefact. Quickshell's
# ThemeState.qml execs `theme data`, so the symlink has to be on PATH.
install:
	go build -o $(PREFIX)/hydra .
	ln -sfn hydra $(PREFIX)/theme
	@echo "installed $(PREFIX)/hydra and the $(PREFIX)/theme symlink"

# Report what a run would do, without changing anything.
plan: build
	./hydra status

clean:
	rm -f hydra
