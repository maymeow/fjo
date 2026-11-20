# fjo — Forgejo CLI

`fjo` is a command-line interface for [Forgejo](https://forgejo.org), designed as an
alternative to GitHub CLI (`gh`) for Forgejo instances: same command style, flags,
JSON output and exit codes where Forgejo supports the feature, with Forgejo-native
semantics where it differs.

> **Status:** v0.2.0, pre-1.0. Authentication, repositories, issues, pull requests and
> releases work against Forgejo 15 and 17; see [CHANGELOG.md](CHANGELOG.md).

## Goals

- Works with any number of self-hosted Forgejo instances.
- Secure by default: tokens live in the OS credential store, never in config files.
- Automation-friendly: stable `--json` output, predictable exit codes, no prompts without a terminal.
- Windows, Linux and macOS.
- No telemetry.

## Quick start

```bash
go install github.com/maymeow/fjo/cmd/fjo@latest
fjo auth login --hostname forgejo.example.com
fjo issue list
```

Prebuilt binaries for Linux, macOS and Windows are on
[GitHub Releases](https://github.com/maymeow/fjo/releases). See [INSTALL.md](INSTALL.md) for installation and [docs/](docs/) for details:

- [Commands](docs/commands.md)
- [Authentication](docs/authentication.md)
- [gh compatibility](docs/compatibility.md)
- [Architecture](docs/architecture.md)
- [Development](docs/development.md)

## License

[MIT](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues: [SECURITY.md](SECURITY.md).
