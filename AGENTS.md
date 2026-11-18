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

## Writing output and data

- **CLI output** goes through `f.IO.Out` / `f.IO.ErrOut` (`internal/iostreams`). `fmt.Fprint*` is fine there; errcheck ignores `fmt.Fprint*` on purpose.
- **Never use `fmt.Fprint`, `fmt.Fprintf` or `fmt.Fprintln` to write to a file (`*os.File`), a network connection (`net.Conn`) or an HTTP response (`http.ResponseWriter`).** Those errors matter (full disk, broken connection) and would go unnoticed. Use one of:
  - `f.Write(...)` / `io.WriteString(...)` and check the error,
  - `io.Copy(dst, src)` and check the error,
  - a `bufio.Writer` (`fmt.Fprintf(bw, ...)` is allowed) followed by a checked `bw.Flush()`.
- The `fmtwrite` linter enforces this. Test files are exempt (fake HTTP servers in tests may use `fmt.Fprint(w, ...)`).

## Lint rules to keep in mind

- Lint rules must pass
- Check every returned error, including `Close`, `os.Remove` and `_ = fn()` (errcheck with `check-blank`). Do not silence errors without a reason.
- Every `//nolint` must name the linter and give a reason: `//nolint:gosec // reason`.
- No `TODO`, `FIXME`, `HACK`, `WORKAROUND` or `TEMP` comments (godox). Finish the work or record it elsewhere.
- No commented-out code.
- Pass a `context.Context` to HTTP requests and external commands (noctx); always close `resp.Body` (bodyclose).
- Compare errors with `errors.Is` / `errors.As`, not `==` or type assertions (errorlint).
- Use `http.MethodGet`, `http.StatusNotFound`, etc. instead of string and number literals (usestdlibvars).
- `unsafe` is forbidden; `internal/api` must not import `internal/cli` (depguard).

## Docummentation

- documentation is in docs/*

## Code Commenting & Clean Code Rules

- **No Inline Comments:** Do not write inline comments inside function bodies explaining what the code is doing. Code must be self-explanatory.
- **Refactor Instead of Explaining:** If you feel the need to write a comment explaining complex logic, refactor the code instead: extract smaller functions with descriptive names and use clear variable identifiers.
- **No Workaround or "Slop" Comments:** Never leave `// TODO`, `// FIXME`, `// HACK`, or workaround comments attempting to justify anti-patterns, temporary fixes, or race conditions. Fix the root cause or handle errors properly.
- **No Commented-Out Code:** Never leave dead or commented-out blocks of code in any file.
- **Allowed Comments Only:** The ONLY allowed comments are GoDoc comments directly above exported types, constants, interfaces, and functions (e.g., `// PackageName handles...`).
