# fjo — Forgejo CLI

`fjo` is a command-line interface for [Forgejo](https://forgejo.org), designed as an
alternative to GitHub CLI (`gh`) for Forgejo instances: same command style, flags,
JSON output and exit codes where Forgejo supports the feature, with Forgejo-native
semantics where it differs.

> **Status:** early development (Phase 0). Only `fjo --help` and `fjo version` work.

## Goals

- Works with any number of self-hosted Forgejo instances.
- Secure by default: tokens live in the OS credential store, never in config files.
- Automation-friendly: stable `--json` output, predictable exit codes, no prompts without a terminal.
- Windows, Linux and macOS.
- No telemetry.

## Quick start

```bash
go build -o bin/ ./cmd/fjo
bin/fjo --help
bin/fjo version
```

See [INSTALL.md](INSTALL.md) for installation and [docs/](docs/) for details:

- [Commands](docs/commands.md)
- [Authentication](docs/authentication.md)
- [gh compatibility](docs/compatibility.md)
- [Architecture](docs/architecture.md)
- [Development](docs/development.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues: [SECURITY.md](SECURITY.md).
