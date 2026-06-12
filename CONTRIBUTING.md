# Contributing to Loba

Thanks for your interest in improving Loba! This guide covers everything you
need to get a change merged.

## Prerequisites

- **Go** (the version pinned in [`go.mod`](go.mod)) and **Git**.
- Optional but recommended: [`golangci-lint`](https://golangci-lint.run/welcome/install/)
  to run the linter locally before pushing.

## Development workflow

```sh
git clone https://github.com/zwenger/TUI-LOBA
cd TUI-LOBA

make build      # build the binary for your platform
make test       # run the test suite with the race detector
make check      # run everything CI runs: vet, lint, vulncheck, test
```

To run the game locally while developing, use the dev launcher — it rebuilds
from your working tree on every run:

```sh
./play.sh host --name You                 # host a game
./play.sh join localhost:7777 --name Two  # join from another terminal
```

## Before you open a pull request

Run `make check` and make sure it passes. CI runs the same steps on every PR:

- `go vet ./...`
- `golangci-lint run ./...`
- `govulncheck ./...`
- `go test -race ./...`
- `go mod tidy` must leave `go.mod`/`go.sum` unchanged.

A PR cannot be merged until all checks are green.

## Commit messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/).
The release changelog is generated from them, so the prefix matters:

- `feat:` a new feature
- `fix:` a bug fix
- `docs:` documentation only
- `ci:` / `chore:` tooling, dependencies, build

Example: `feat: add spectator mode to the lobby`

## Code conventions

- Keep the server **authoritative**: all game rules live in `internal/game`;
  clients only render state and send commands. Never trust client input.
- Match the style of the surrounding code; comments explain *why*, not *what*.
- Add tests for new behavior. The game engine and protocol especially should
  stay well covered.

## Reporting bugs and requesting features

Open an issue using the provided templates. For anything security-sensitive,
follow [SECURITY.md](SECURITY.md) instead of opening a public issue.
