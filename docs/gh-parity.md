# gh command coverage

Which `gh` commands fjo implements, which are planned and which have no
Forgejo equivalent. Behaviour differences of implemented commands are listed
in [compatibility.md](compatibility.md).

Status:

- **done**: implemented and tested against Forgejo 15 and 17.
- **differs**: implemented, but with a difference a script may notice (see the note).
- **planned (Phase N)**: on the roadmap in IMPLEMENTATION.txt §16.
- **idea (B-NN)**: recorded in docs/BACKLOG.txt, not scheduled.
- **n/a**: Forgejo has no equivalent feature.

## Summary

| Group | gh subcommands | done | planned / idea | n/a |
|---|---:|---:|---:|---:|
| `auth` | 7 | 5 | 2 | 0 |
| `repo` | 16 | 12 | 3 | 1 |
| `issue` | 15 | 10 | 3 | 2 |
| `pr` | 18 | 13 | 4 | 1 |
| `release` | 10 | 8 | 0 | 2 |
| Actions (`run`, `workflow`, `cache`) | 14 | 0 | 10 | 4 |
| Other top-level commands | 23 | 3 | 12 | 8 |
| **Total** | **103** | **51** | **34** | **18** |

API availability in the "n/a" and "idea" notes was checked against the
pinned Forgejo 15.0.11 and 17.0.1 swagger files (`test/fixtures/swagger`).

## Porting a gh script

Most scripts that use the commands marked **done** work after replacing `gh`
with `fjo`. Check these points:

1. **Environment variables**: `GH_TOKEN`, `GH_HOST`, `GH_REPO`, `GH_PAGER`,
   `GH_PROMPT_DISABLED` become `FJO_TOKEN`, `FJO_HOST`, `FJO_REPO`, `FJO_PAGER`,
   `FJO_PROMPT_DISABLED`. fjo does not read the `GH_*` names.
2. **Host**: gh defaults to github.com. fjo has no default host. Outside a
   clone with a Forgejo remote, set `FJO_HOST`, `defaults.host` in the config
   or pass `-R HOST/OWNER/REPO`.
3. **Exit codes**: `0`, `2`, `4` and `8` mean the same as in gh. Errors that gh
   reports as `1` are split into `10`–`16` (usage, forbidden, not found,
   conflict, network, unsupported, timeout). `if ! fjo ...` and `$? -ne 0`
   work unchanged; `$? -eq 1` does not.
4. **JSON values**: `state` is lowercase (`open`, `closed`, `merged`); gh
   uses `OPEN`, `CLOSED`, `MERGED`. User objects contain only `login`.
   `comments` is a count, also in `view`. GitHub-only fields do not exist,
   and asking for one with `--json` is a usage error (exit 10), not an empty
   value.
5. **`api`**: the paths are Forgejo REST paths. There is no GraphQL, and
   `api` has no `--jq` or `--template` yet (B-49); pipe to `jq` instead.
   `--paginate` merges array pages into one JSON array.
6. **Repository detection**: only remotes on a known Forgejo host count, and
   `origin` is not assumed. With several candidate repositories fjo stops
   with an error until `fjo repo set-default` picks one; gh would choose.
7. **Draft pull requests** are a `WIP:` title prefix in Forgejo.
   `pr create --draft` and `pr ready` change the title.
8. **Branch lookup** (`pr view <branch>`, `pr checks`, no argument) finds open
   pull requests only; gh also finds the latest closed or merged one (B-47).

Without a terminal fjo never prompts, as with `GH_PROMPT_DISABLED`.
Destructive commands need `--yes`.

## auth

| gh | fjo | Status |
|---|---|---|
| `auth login` | `auth login` | done |
| `auth logout` | `auth logout` | done |
| `auth status` | `auth status` | done |
| `auth switch` | `auth switch` | done |
| `auth token` | `auth token` | done |
| `auth refresh` | — | idea (B-16, needs OAuth) |
| `auth setup-git` | — | idea; private HTTPS clones need a Git credential helper configured outside fjo |

## repo

| gh | fjo | Status |
|---|---|---|
| `repo list` | `repo list` | differs: owned repositories only; no `--language`, `--topic`, `--visibility internal` |
| `repo view` | `repo view` | differs: no README rendering (B-50) |
| `repo create` | `repo create` | differs: `--initialize` instead of `--add-readme`; no `--source`, `--push`, `--template` |
| `repo clone` | `repo clone` | done |
| `repo fork` | `repo fork` | differs: no `--remote` mode |
| `repo edit` | `repo edit` | differs: description, homepage, visibility, default branch only |
| `repo rename` | `repo rename` | differs: does not update the local remote (B-39) |
| `repo delete` | `repo delete` | done |
| `repo archive` | `repo archive` | done |
| `repo unarchive` | `repo unarchive` | done |
| `repo sync` | `repo sync` | differs: no `--force` |
| `repo set-default` | `repo set-default` | done |
| `repo deploy-key` | — | planned (Phase 6) |
| `repo autolink` | — | n/a; Forgejo has no autolink API |
| `repo gitignore` | — | idea; Forgejo has `/gitignore/templates` |
| `repo license` | — | idea; Forgejo has `/licenses` |

## issue

| gh | fjo | Status |
|---|---|---|
| `issue list` | `issue list` | done |
| `issue view` | `issue view` | differs: `comments` in JSON is a count |
| `issue create` | `issue create` | differs: `--web` cannot prefill metadata (exit 15) |
| `issue edit` | `issue edit` | done |
| `issue close` | `issue close` | differs: `--reason` returns exit 15 |
| `issue reopen` | `issue reopen` | done |
| `issue comment` | `issue comment` | done |
| `issue delete` | `issue delete` | done |
| `issue pin` | `issue pin` | done |
| `issue unpin` | `issue unpin` | done |
| `issue status` | — | idea (B-24) |
| `issue lock` / `unlock` | — | idea; API only in Forgejo 17 (`PUT/DELETE /issues/{index}/lock`) |
| `issue transfer` | — | n/a; Forgejo can transfer repositories, not issues |
| `issue develop` | — | n/a (GitHub linked branches) |

`issue lock` and `issue unlock` count as two subcommands in the summary.

## pr

| gh | fjo | Status |
|---|---|---|
| `pr list` | `pr list` | differs: merged, draft and assignee filtered on the client; `--base`/`--head` on the server only in Forgejo 17 |
| `pr view` | `pr view` | differs: branch lookup finds open PRs only (B-47) |
| `pr diff` | `pr diff` | done |
| `pr checks` | `pr checks` | differs: Forgejo commit statuses; no `--watch` (B-45) |
| `pr create` | `pr create` | differs: `--draft` adds a `WIP:` prefix |
| `pr checkout` | `pr checkout` | done |
| `pr merge` | `pr merge` | done; methods follow the repository settings |
| `pr close` | `pr close` | done |
| `pr reopen` | `pr reopen` | done |
| `pr ready` | `pr ready` | differs: removes the `WIP:` prefix |
| `pr edit` | `pr edit` | done |
| `pr comment` | `pr comment` | done |
| `pr review` | `pr review` | differs: no inline comments |
| `pr status` | — | idea (B-24) |
| `pr lock` / `unlock` | — | idea; same endpoint as issues, Forgejo 17 only |
| `pr revert` | — | n/a; Forgejo has no revert API |
| `pr update-branch` | — | idea; Forgejo 15 and 17 have `POST /pulls/{index}/update` |

`pr lock` and `pr unlock` count as two subcommands in the summary.

## release

| gh | fjo | Status |
|---|---|---|
| `release list` | `release list` | done |
| `release view` | `release view` | done |
| `release create` | `release create` | differs: no `--generate-notes`, `--notes-start-tag`, `--latest`, `--discussion-category` |
| `release edit` | `release edit` | done |
| `release delete` | `release delete` | done |
| `release upload` | `release upload` | done |
| `release delete-asset` | `release delete-asset` | done |
| `release download` | `release download` | differs: verifies SHA-256 when the release has a checksum manifest |
| `release verify` | — | n/a (GitHub attestations) |
| `release verify-asset` | — | n/a (GitHub attestations) |

## Actions

Planned for Phase 5 (B-10). The Forgejo Actions API differs a lot between
versions: Forgejo 15 can list and view runs and dispatch workflows; Forgejo
17 adds jobs, logs, rerun, cancel, delete and artifacts. Commands missing on
the server will exit 15.

| gh | fjo | Status | Forgejo API |
|---|---|---|---|
| `run list` | same | planned (Phase 5) | 15, 17 |
| `run view` | same | planned (Phase 5) | 15, 17; jobs and `--log` (planned as `run logs`) only 17 |
| `run watch` | same | planned (Phase 5) | 15, 17 (polling) |
| `run rerun` | same | planned (Phase 5) | 17 only |
| `run cancel` | same | planned (Phase 5) | 17 only |
| `run download` | same | planned (Phase 5) | 17 only (artifacts) |
| `run delete` | same | planned (Phase 5) | 17 only |
| `workflow run` | same | planned (Phase 5) | 15, 17 (`dispatches`) |
| `workflow list`, `workflow view` | — | idea: read `.forgejo/workflows` through the contents API | no workflow endpoint |
| `workflow enable`, `workflow disable` | — | n/a | no API |
| `cache list`, `cache delete` | — | n/a | no API |

## Other top-level commands

| gh | fjo | Status |
|---|---|---|
| `api` | `api` | differs: REST only, no `--jq`/`--template` yet (B-49) |
| `completion` | `completion` | done |
| `version`, `--version` | same | done |
| `alias` (`set`, `list`, `delete`, `import`) | — | idea (B-23, Phase 7) |
| `browse` | — | idea (B-21) |
| `config` (`get`, `set`, `list`, `clear-cache`) | — | idea (B-26) |
| `extension` | — | planned (Phase 7) |
| `label` (`list`, `create`, `edit`, `delete`, `clone`) | — | idea (B-22) |
| `org list` | — | planned (Phase 6) |
| `search` (`repos`, `issues`, `prs`, `code`, `commits`) | — | idea (B-25) |
| `secret` (`list`, `set`, `delete`) | `repo secret` | planned (Phase 6), under `repo` |
| `variable` (`list`, `get`, `set`, `delete`) | `repo variable` | planned (Phase 6), under `repo` |
| `ssh-key` (`list`, `add`, `delete`) | same | planned (Phase 6) |
| `gpg-key` (`list`, `add`, `delete`) | — | idea; Forgejo has a GPG key API |
| `status` | — | idea (B-24) |
| `ruleset` | — | n/a; Forgejo has branch protection, not rulesets |
| `attestation` | — | n/a |
| `codespace` | — | n/a |
| `copilot` | — | n/a |
| `gist` | — | n/a |
| `project` | — | n/a |
| `cache` | — | see Actions |
| `preview` | — | n/a |
| `agent-task` | — | n/a |

fjo-only commands planned in Phase 6 (IMPLEMENTATION §5.8): `org view/create/edit/teams/members`,
`user view/search/keys`, `repo webhook`.
