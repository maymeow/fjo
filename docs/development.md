# Development

## Requirements

- Go (version in `go.mod`), Git.
- Linters: `staticcheck`, `govulncheck`
  (`go install honnef.co/go/tools/cmd/staticcheck@v0.8.2`,
  `go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`).
- `go test -race` needs cgo and a C compiler; on Windows install MinGW-w64 gcc.

## Tasks

| Task | Linux / macOS | Windows |
|---|---|---|
| Build | `make build` | `./scripts/build.ps1 build` |
| Unit tests | `make test` | `./scripts/build.ps1 test` |
| Race tests | `make race` | `./scripts/build.ps1 race` |
| All release checks | `make check` | `./scripts/build.ps1 check` |

## Project documents

- `docs/IMPLEMENTATION.txt` — the plan and contracts.
- `docs/CHANGELOG.txt` — development log (where we are).
- `docs/BACKLOG.txt` — ideas, open decisions, tech debt.
- `CHANGELOG.md` — user-facing changes.

## CI runners

CI runs on Forgejo Actions on the project's own instance
(`.forgejo/workflows/ci.yaml`) with self-hosted runners:

| Label | Platform | Needs |
|---|---|---|
| `fjo-linux-amd64` | Linux amd64 | Go, git, gcc; Docker for Forgejo integration tests (Phase 1+) |
| `fjo-windows-amd64` | Windows amd64 | Go, git, Git Bash, MinGW-w64 gcc |
| `fjo-macos-arm64` | macOS arm64 | Go, git, Xcode command-line tools |

Security:

- Runners must not have access to production credentials.
- Require maintainer approval before running workflows from forks.
- Prefer ephemeral or regularly reset runner workspaces.

Runner registration: see the Forgejo runner documentation for your instance version
(`forgejo-runner register --labels fjo-linux-amd64:host ...`).
