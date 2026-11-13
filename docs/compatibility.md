# Compatibility

## Forgejo versions

| Forgejo | Status |
|---|---|
| 15 LTS | required test target |
| 17 stable | required test target |

Other versions may work but are not verified. Capabilities are detected per
instance, not inferred from the version number alone (IMPLEMENTATION §9.5).

## GitHub CLI (gh) compatibility policy

fjo is an alternative to `gh` for Forgejo (IMPLEMENTATION §1.4):

- Same command and flag names where Forgejo has the feature.
- Same `--json <fields>` / `--jq` / `--template` behavior and JSON field names
  where the meaning is equivalent.
- Same exit-code meanings as gh; fjo-specific codes only above gh's range.
- `FJO_*` environment variables mirror `GH_*`.
- Forgejo semantics win where they differ. Every intentional difference is listed below.

## Exit codes

| Code | Meaning | gh |
|---:|---|---|
| 0 | Success | same |
| 1 | General failure | same |
| 2 | Cancelled by user (prompt aborted) | same |
| 4 | Authentication required or failed | same |
| 8 | Checks pending (reserved for `pr checks`) | same |
| 10 | Invalid usage or input (unknown command, bad flag, wrong arguments) | gh uses 1 |
| 11 | Authorization denied (HTTP 403) | gh uses 1 |
| 12 | Resource not found | gh uses 1 |
| 13 | Conflict or rejected operation | gh uses 1 |
| 14 | Network failure or service unavailable | gh uses 1 |
| 15 | Feature not supported by this Forgejo instance | — |
| 16 | Operation timed out | gh uses 1 |
| 130 | Interrupted (Ctrl+C) | — |

Scripts written for gh that test `$? -ne 0`, `== 2`, `== 4` or `== 8` behave the same.

## Environment variables

| fjo | gh equivalent | Status |
|---|---|---|
| `FJO_TOKEN` | `GH_TOKEN` | planned (Phase 1); already redacted from all output |
| `FJO_HOST` | `GH_HOST` | resolved, used from Phase 1 |
| `FJO_REPO` | `GH_REPO` | planned (Phase 2) |
| `FJO_CONFIG_DIR` | `GH_CONFIG_DIR` | done |
| `FJO_EDITOR` | `GH_EDITOR` | resolved, used from Phase 3 |
| `FJO_PAGER` | `GH_PAGER` | resolved, used from Phase 3 |
| `FJO_PROMPT_DISABLED` | `GH_PROMPT_DISABLED` | done |
| `FJO_DEBUG` | `GH_DEBUG` | done |
| `FJO_NO_COLOR`, `NO_COLOR`, `CLICOLOR_FORCE` | `NO_COLOR`, `CLICOLOR_FORCE` | done |
| `XDG_CONFIG_HOME` | `XDG_CONFIG_HOME` | done (all platforms) |

## Command parity

Status: **done**, **planned** (phase), **n/a** (no Forgejo equivalent), **differs** (see notes).

| gh | fjo | Status |
|---|---|---|
| `gh --version`, `gh version` | `fjo --version`, `fjo version` | done |
| `gh help`, `gh completion` | same | done (cobra built-in) |
| `gh auth login/logout/status/switch/token` | same | planned (Phase 1) |
| `gh auth refresh` | — | n/a until OAuth (BACKLOG B-16) |
| `gh api` | `fjo api` | planned (Phase 1) |
| `gh config get/set/list` | same | idea (BACKLOG B-26) |
| `gh repo ...` | same | planned (Phase 2) |
| `gh browse` | same | idea (BACKLOG B-21) |
| `gh issue ...` | same | planned (Phase 3) |
| `gh pr ...` | same | planned (Phase 3); draft = `WIP:` title prefix (differs) |
| `gh release ...` | same | planned (Phase 4) |
| `gh run`, `gh workflow` | same | planned (Phase 5), scope depends on Forgejo Actions API (BACKLOG B-10) |
| `gh org`, `gh ssh-key`, `gh secret`, `gh variable` | same | planned (Phase 6) |
| `gh label` | same | idea (BACKLOG B-22) |
| `gh search` | same | idea (BACKLOG B-25) |
| `gh status` | same | idea (BACKLOG B-24) |
| `gh alias` | same | idea (BACKLOG B-23) |
| `gh extension` | same | planned (Phase 7) |
| `gh gist`, `gh codespace`, `gh project`, `gh copilot` | — | n/a |

## Intentional differences from gh

| Area | Difference | Reason |
|---|---|---|
| Exit codes 10–16 | fjo distinguishes usage, 403, 404, conflict, network, unsupported, timeout | Better automation; gh codes unchanged |
| Host identity | Hosts may include a port or base path (`example.com/forgejo`) | Self-hosted Forgejo runs on custom ports and subpaths |
| Config format | Single `config.yaml` with `hosts:` map | Tokens are never stored in it |
