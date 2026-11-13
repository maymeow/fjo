# Architecture

Full design: [IMPLEMENTATION.txt](IMPLEMENTATION.txt) §3–§4. This page describes
what exists in the code today.

## Layers

```text
cmd/fjo/main.go          -> cli.Main(): signal handling, exit code
internal/cli             -> cobra commands (thin)
internal/app             -> Factory: shared dependencies, lazy config
internal/config          -> config.yaml model, host IDs, validation, precedence, atomic save
internal/iostreams       -> stdin/stdout/stderr, TTY + color detection, prompt policy
internal/logging         -> slog logger with secret redaction
internal/fjoerr          -> typed errors (Kind) and exit-code mapping
internal/build           -> version metadata (ldflags + Go build info)
```

Planned (later phases): `internal/forgejo` (API client), `internal/credentials`,
`internal/git`, `internal/output`, `internal/browser`, `internal/extensions`.

## Rules

- Commands get dependencies from `*app.Factory`; they never read `os.Stdout`,
  `os.Getenv` or the filesystem directly.
- Every error that reaches the user is an `*fjoerr.Error` (or classified by
  `fjoerr.KindOf`); the root maps it to an exit code (see compatibility.md).
- Errors and diagnostics go to stderr. stdout carries only command results.
- All error messages and log records pass through `logging.Redactor`.
  `FJO_TOKEN` is registered as a secret at startup.
- Commands that must work with a broken config (e.g. `version`) set the
  annotation `fjo/no-config: "true"`.

## Configuration

- Location: `FJO_CONFIG_DIR` > `$XDG_CONFIG_HOME/fjo` > OS config dir + `/fjo`.
- Precedence: flag > env > repo-local (Phase 2) > config.yaml > built-in default.
- Unknown keys are errors. Host keys must be canonical host IDs (§6.2).
- Writes are atomic (temp file + rename), file mode 0600, directory 0700.
