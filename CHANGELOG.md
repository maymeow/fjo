# Changelog

All notable user-facing changes to fjo are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project uses [Semantic Versioning](https://semver.org/).

Development progress is tracked in [docs/CHANGELOG.txt](docs/CHANGELOG.txt).

## [Unreleased]

### Added

- `fjo release create`, `edit` and `delete` manage releases by tag, including draft publication and optional Git tag cleanup.

- `fjo release list` and `fjo release view` show release notes, assets and metadata, with structured output for scripts.

- `fjo release upload` uploads release assets without loading them into memory, shows progress in a terminal and replaces existing assets with `--clobber` without losing the old asset when an upload fails.

- `fjo release delete-asset` deletes a release asset after confirmation (`--yes` without a terminal).

- `fjo release download` downloads release assets or the source archive, verifies sizes and SHA-256 checksums from a checksum manifest in the release, never leaves a partial file behind, and refuses unsafe asset names.

- `fjo release create <tag> <files>...` uploads the files as assets and publishes the release only after all uploads succeeded.

### Changed

- `--web` on `issue` and `pr` commands now says which URL it opens and, when the browser fails to start, suggests opening the URL manually or setting `FJO_BROWSER`, as `repo view --web` already did.

## [0.1.0] - 2026-11-18

First release. Supports Forgejo 15 and 17.

### Added

- Authentication: `fjo auth login/status/switch/logout/token` with tokens in the OS
  credential store (`--insecure-storage` for hosts without one), `FJO_TOKEN`, several
  accounts per host, and hosts with custom ports or base paths.
- Repositories: `fjo repo list/view/create/clone/fork/edit/rename/delete/archive/unarchive/sync`
  and `fjo repo set-default`, with repository detection from Git remotes.
- Issues: `fjo issue list/view/create/edit/close/reopen/comment/delete/pin/unpin`,
  including filters, issue URLs, body files, editor support, interactive prompts and
  browser shortcuts.
- Pull requests: `fjo pr list/view/diff/checks/create/checkout/merge/review/close/reopen/ready/edit/comment`.
  `pr review` approves, requests changes or comments; `pr checks` exits 8 while checks
  are pending, and 1 when a check failed or none were reported.
- `fjo api` for authenticated REST requests, with `{owner}`, `{repo}` and `{branch}`
  placeholders and `--paginate`.
- Structured output: `--json`, `--jq` and `--template` for repository, issue, pull
  request and check output; tables on a terminal and TSV otherwise; pager and editor
  support (`FJO_PAGER`, `FJO_EDITOR`).
- `fjo --help`, `fjo --version`, `fjo version` and shell completion.
- Global flags `--color`, `--verbose`, `--debug`; tokens never appear in debug output.
- User configuration file (`config.yaml`) with validation; location overridable by
  `FJO_CONFIG_DIR` or `XDG_CONFIG_HOME`.
- gh-compatible exit codes, plus fjo codes 10–16 (see docs/compatibility.md).

[Unreleased]: https://github.com/maymeow/fjo/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/maymeow/fjo/releases/tag/v0.1.0
