# Changelog

All notable user-facing changes to fjo are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project uses [Semantic Versioning](https://semver.org/).

Development progress is tracked in [docs/CHANGELOG.txt](docs/CHANGELOG.txt).

## [Unreleased]

### Added

- `fjo issue list/view/create/edit/close/reopen/comment/delete/pin/unpin`,
  including filters, issue URLs, JSON/jq/templates, body files, editor support,
  interactive prompts and browser shortcuts (Forgejo 15 and 17).

- `fjo --help` and `fjo --version`.
- `fjo version` showing version, commit, Go version and platform.
- Global flags `--color`, `--verbose`, `--debug`.
- User configuration file (`config.yaml`) with validation; location overridable by
  `FJO_CONFIG_DIR` or `XDG_CONFIG_HOME`.
- gh-compatible exit codes (see docs/compatibility.md).
