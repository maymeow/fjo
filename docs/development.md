# Development

## Requirements

- Go (version in `go.mod`), Git.
- Linters: `staticcheck`, `govulncheck`
  (`go install honnef.co/go/tools/cmd/staticcheck@v0.8.2`,
  `go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`).
- `go test -race` needs cgo and a C compiler; on Windows install MinGW-w64 gcc.
- Docker for the opt-in integration tests against Forgejo 15 and 17.

## Tasks

| Task | Linux / macOS | Windows |
|---|---|---|
| Build | `make build` | `./scripts/build.ps1 build` |
| Unit tests | `make test` | `./scripts/build.ps1 test` |
| Integration tests (Docker) | `make test-integration` | — (Linux runner/CI) |
| Race tests | `make race` | `./scripts/build.ps1 race` |
| All release checks | `make check` | `./scripts/build.ps1 check` |

Integration tests are opt-in and excluded from `go test ./...` by the
`integration` build tag. `make test-integration` starts Forgejo 15.0.11 and
17.0.1 containers, creates a temporary admin token and runs
`go test -tags=integration ./test/integration`. The images are pulled on the
first run; set `FJO_TEST_FORGEJO_VERSIONS` to test a subset. The test package
can also target an existing instance through `FJO_TEST_FORGEJO_URL`,
`FJO_TEST_FORGEJO_TOKEN` and (optionally) `FJO_TEST_FORGEJO_VERSION`; it skips
when the URL or token is missing.

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

The same jobs (test matrix on Linux, Windows and macOS, Forgejo integration
tests, lint and govulncheck) also run on GitHub Actions with GitHub-hosted
runners (`.github/workflows/ci.yaml`). Keep both workflow files in sync when
you change CI.
