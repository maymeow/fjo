# AGENTS.md

Instructions for AI coding agents working on `fjo`.

## Project

- `fjo` is a Go CLI (module `github.com/maymeow/fjo`); the entry point is `cmd/fjo`.
- All application code lives under `internal/` (`api`, `cli/...`, `config`, `git`, `iostreams`, `output`, ...).
- `tools/fmtwrite` is a separate Go module holding a custom golangci-lint plugin. It is not part of the `fjo` build.

## Commands

| Task | Command |
|---|---|
| Build | `make build` |
| Unit tests | `make test` |
| Race tests | `make race` |
| Format | `make fmt` |
| Lint | `make lint` |
| Full release check | `make check` |
| Integration tests (Docker) | `make test-integration` |

- `make lint` builds `bin/golangci-lint-fjo` (golangci-lint v2.15.0 with project plugins, see `.custom-gcl.yml`) and runs it. Do not run plain `golangci-lint run`; it does not know the `fmtwrite` linter and fails.
- Run `make fmt`, `make test` and `make lint` before you call a change done.

## Git workflow

- **Never work on `main`.** Do not commit to `main` and never push to it directly.
- Before making any change, check the current branch (`git branch --show-current`). If it is `main`, create a new branch first (e.g. `git switch -c phase-3d-pr-write`).
- Changes reach `main` only through a reviewed pull request.

## Shared helpers

- Helpers used by more than one command package live in `internal/cli/cmdutil` (`AddRepoFlag`, `JSONOptions`, `TableOptions`, `Page`, `OpenBrowser`, `OneLine`, ...); formatting helpers live in `internal/output`. Check both before writing a new helper.
- Do not create a new command package by copying an existing one. Start from the shared helpers and add only what is specific to the new commands.
- `TestNoDuplicateFunctions` (`internal/cli/duplicates_test.go`) fails when two functions in `internal/` have identical bodies. Move the helper to a shared package instead. Only when the functions must stay separate, add them to `allowedDuplicates` with a comment giving the reason.

## Writing output and data

- **CLI output** goes through `f.IO.Out` / `f.IO.ErrOut` (`internal/iostreams`). `fmt.Fprint*` is fine there; errcheck ignores `fmt.Fprint*` on purpose.
- **Never use `fmt.Fprint`, `fmt.Fprintf` or `fmt.Fprintln` to write to a file (`*os.File`), a network connection (`net.Conn`) or an HTTP response (`http.ResponseWriter`).** Those errors matter (full disk, broken connection) and would go unnoticed. Use one of:
  - `f.Write(...)` / `io.WriteString(...)` and check the error,
  - `io.Copy(dst, src)` and check the error,
  - a `bufio.Writer` (`fmt.Fprintf(bw, ...)` is allowed) followed by a checked `bw.Flush()`.
- The `fmtwrite` linter enforces this. Test files are exempt (fake HTTP servers in tests may use `fmt.Fprint(w, ...)`).

## Lint rules to keep in mind

- `make lint` must report no new issues in code you added or changed. Fix the cause; do not add `//nolint` just to make it pass.
- Check every returned error, including `Close`, `os.Remove` and `_ = fn()` (errcheck with `check-blank`). Do not silence errors without a reason.
- Every `//nolint` must name the linter and give a reason: `//nolint:gosec // reason`.
- Pass a `context.Context` to HTTP requests and external commands (noctx); always close `resp.Body` (bodyclose).
- Compare errors with `errors.Is` / `errors.As`, not `==` or type assertions (errorlint).
- Use `http.MethodGet`, `http.StatusNotFound`, etc. instead of string and number literals (usestdlibvars).
- `unsafe` is forbidden; `internal/api` must not import `internal/cli` (depguard).

## Documentation

- User documentation lives in `docs/` (`commands.md`, `authentication.md`, `compatibility.md`, `gh-parity.md`, `architecture.md`, `development.md`). When you change a command's behaviour, flags or output, update the relevant page in the same change. When you add a command, update its status in `docs/gh-parity.md`.
- `docs/IMPLEMENTATION.txt` is the specification; follow it.
- **`CHANGELOG.md`** (user-facing, [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)): add every user-visible change under `## [Unreleased]` in the matching section (`### Added`, `### Changed`, `### Fixed`, `### Removed`). Write it from the user's point of view; internal refactors and lint fixes do not belong here.
- **`docs/CHANGELOG.txt`** (internal development log): record what was actually implemented and update its "Current status" block.
- **`docs/BACKLOG.txt`**: new ideas, scope changes, open decisions and unfinished work go here, never into code comments.

## Code Commenting & Clean Code Rules

- **No "what" comments:** Do not write comments inside function bodies explaining what the code is doing. Code must be self-explanatory.
- **Refactor instead of explaining:** If you feel the need to explain complex logic, refactor instead: extract smaller functions with descriptive names and use clear variable identifiers.
- **Short "why" comments are allowed:** A one-line comment inside a function body is allowed only when it explains a non-obvious reason the code cannot express (e.g. why an error is deliberately ignored, why an external command is safe).
- **No workaround or "slop" comments:** Never leave `TODO`, `FIXME`, `HACK`, `WORKAROUND` or `TEMP` comments (enforced by godox), nor comments justifying anti-patterns, temporary fixes or race conditions. Fix the root cause or handle errors properly; record unfinished work in `docs/BACKLOG.txt`.
- **No commented-out code:** Never leave dead or commented-out code in any file (enforced by gocritic `commentedOutCode`).
- **Allowed comments:** Apart from the "why" comments above, only these are allowed:
  - GoDoc comments directly above exported types, constants, variables, interfaces and functions, starting with the name (`// Client talks to ...`); required for every exported identifier,
  - doc comments above unexported functions and types when their contract is not obvious from the name and signature (same `// name ...` form),
  - package comments above the `package` clause (`// Package api implements ...`),
  - compiler directives (`//go:build`, `//go:generate`, `//go:embed`),
  - `//nolint:<linter> // <reason>`,
  - `// want ...` annotations in analyzer testdata (`tools/*/testdata`).
