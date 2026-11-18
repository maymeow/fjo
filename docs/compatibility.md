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
| `FJO_TOKEN` | `GH_TOKEN` | done (applies to the targeted host, never stored; redacted from all output) |
| `FJO_HOST` | `GH_HOST` | done |
| `FJO_REPO` | `GH_REPO` | done (`[HOST/]OWNER/REPO`) |
| `FJO_CONFIG_DIR` | `GH_CONFIG_DIR` | done |
| `FJO_EDITOR` | `GH_EDITOR` | done (Phase 3a: `--editor`, body flags) |
| `FJO_PAGER` | `GH_PAGER` | done (Phase 3a: long-output commands, `--no-pager`) |
| `FJO_PROMPT_DISABLED` | `GH_PROMPT_DISABLED` | done |
| `FJO_DEBUG` | `GH_DEBUG` | done |
| `FJO_NO_COLOR`, `NO_COLOR`, `CLICOLOR_FORCE` | `NO_COLOR`, `CLICOLOR_FORCE` | done |
| `XDG_CONFIG_HOME` | `XDG_CONFIG_HOME` | done (all platforms) |

## JSON output

`--json FIELDS`, `--jq EXPR` and `--template TPL` follow gh: fields are
comma-separated, bare `--json` prints the sorted field list as a usage error,
and `--jq`/`--template` require `--json`. The output is a normalized fjo
model (IMPLEMENTATION §10.2), never the raw Forgejo response. JSON is compact
when stdout is not a terminal and indented when it is.

Field names follow gh where the meaning is equivalent. Known differences:

| Area | Difference | Reason |
|---|---|---|
| `repo list --json`, `repo view --json` | `name` is the short name, `nameWithOwner` is `owner/repo`; `parent` is `{nameWithOwner}`; `primaryLanguage` is a string; GitHub-only fields (`licenseInfo`, `viewerPermission`, `defaultBranchRef`, ...) are absent | Forgejo's repository model |
| Issue/PR `state` | Lowercase `open`, `closed`, `merged`, as in the IMPLEMENTATION §10.2 example | Forgejo state values; gh uses uppercase GraphQL states |
| Issue/PR `author`, `assignees`, `reviewers` | A user object contains only `login` | gh's user objects carry many GraphQL-only fields |
| Issue/PR `comments` | A count in both list and view; gh returns the comment array in `view` | Forgejo returns the count separately; `view --comments` renders the discussion as plain text |
| `Comment.id`, `Review.id` | Integer database IDs | Forgejo REST IDs are integers; gh exposes GraphQL node IDs |
| `Check.state` | Forgejo commit-status strings | `pr checks` maps them to gh-style buckets |

### Issue and pull request fields

Every field below can be selected with `--json`. Times are RFC 3339 strings;
nullable fields are `null` when unset.

| Field | Type | Issue | PR | Notes |
|---|---|---|---|---|
| `number` | integer | ✓ | ✓ | Repository-local index |
| `title` | string | ✓ | ✓ | |
| `state` | string | ✓ | ✓ | `open`, `closed`; PRs also `merged` |
| `isDraft` | boolean | | ✓ | Forgejo `draft` or a `WIP:`/`[WIP]` title prefix |
| `author` | `{login}` | ✓ | ✓ | |
| `url` | string | ✓ | ✓ | Web URL |
| `body` | string | ✓ | ✓ | Markdown source |
| `labels` | `[{name, color}]` | ✓ | ✓ | |
| `milestone` | `{title, number}` or null | ✓ | ✓ | `number` is the Forgejo milestone ID |
| `assignees` | `[{login}]` | ✓ | ✓ | |
| `reviewers` | `[{login}]` | | ✓ | Requested user reviewers |
| `comments` | integer | ✓ | ✓ | Count, also in `view` |
| `baseRefName` | string | | ✓ | |
| `headRefName` | string | | ✓ | |
| `headRepository` | string | | ✓ | `owner/name` |
| `isCrossRepository` | boolean | | ✓ | Head and base are different repositories |
| `mergeable` | string | | ✓ | `MERGEABLE`, `CONFLICTING` or `UNKNOWN` |
| `additions`, `deletions`, `changedFiles` | integer | | ✓ | From Forgejo's PR response |
| `createdAt`, `updatedAt` | time | ✓ | ✓ | |
| `closedAt` | time or null | ✓ | ✓ | |
| `mergedAt` | time or null | | ✓ | |

`pr checks --json` returns check objects with `name`, `state` (Forgejo
commit-status string), `bucket` (`pass`, `fail`, `pending`, `skipping`),
`description`, `url`, `startedAt` and `completedAt` (null while pending).

Templates provide the gh helper subset `tablerow`, `tablerender`, `timeago`,
`color`, `join`, `pluck` and `truncate`. `truncate` is display-width aware
(wide characters count twice, combining marks zero) and ends an overlong
value with `…`.

## Command parity

Status: **done**, **planned** (phase), **n/a** (no Forgejo equivalent), **differs** (see notes).

| gh | fjo | Status |
|---|---|---|
| `gh --version`, `gh version` | `fjo --version`, `fjo version` | done |
| `gh help`, `gh completion` | same | done (cobra built-in) |
| `gh auth login/logout/status/switch/token` | same | done |
| `gh auth refresh` | — | n/a until OAuth (BACKLOG B-16) |
| `gh api` | `fjo api` | done (incl. `{owner}`/`{repo}`/`{branch}`); differs: `--paginate`, no GraphQL/`--jq` yet (see below) |
| `gh config get/set/list` | same | idea (BACKLOG B-26) |
| `gh repo list/view/set-default` | same | done (Phase 2a; `--json`/`--jq`/`--template` from 3a) |
| `gh repo create/clone/fork/edit/rename/delete/archive/unarchive/sync` | same | done (Phase 2b); differences below |
| `gh browse` | same | idea (BACKLOG B-21) |
| `gh issue list/view/create/edit/close/reopen/comment/delete/pin/unpin` | same | done (Phase 3b); differences below |
| `gh pr list/view/diff/checks` | same | done (Phase 3c), with differences below |
| `gh pr create/checkout/merge/close/reopen/ready/edit/comment` | same | done (Phase 3d), with differences below |
| `gh pr review` | same | done (Phase 3e), with differences below |
| `gh release list/view` | same | done (Phase 4a) |
| `gh release create/edit/delete` | same | done (Phase 4b) |
| `gh release upload/delete-asset/download` | same | planned (Phases 4c–4d) |
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
| `api --paginate` | Pages that are JSON arrays are merged into one array (gh prints the pages one after another) | Output stays valid JSON for `jq` and scripts |
| `api` GraphQL, `--jq`, `--template`, `--cache`, `--preview` | Not available | Forgejo has no GraphQL API; `api` output filters are tracked in BACKLOG B-49 |
| `api --input` | Sets `Content-Type: application/json` unless `-H` overrides it | Forgejo API endpoints expect JSON |
| Repository detection | Only remotes on a known Forgejo host (configured hosts, `FJO_HOST`) are considered; several different repositories are an error until `fjo repo set-default` picks one. gh prefers `upstream` > `github` > `origin` and prompts | `origin` is not assumed (IMPLEMENTATION §8.2); no silent wrong choice |
| `repo set-default` | Stored as `remote.<name>.fjo-resolved` in `.git/config` (gh: `gh-resolved`) | Same mechanism, own key so gh and fjo do not interfere |
| `repo list` | Lists repositories **owned** by the user/org (Forgejo `/repos/search?uid=&exclusive=true`), newest update first; `--visibility internal`, `--language`, `--topic` not supported | Forgejo search API filters |
| `repo view` | No README rendering; non-TTY output is `key:\tvalue` lines | Markdown rendering is tracked in BACKLOG B-50 |
| `repo create` | Without `--private`/`--public` the server default visibility is used; `--initialize` is the `--add-readme` equivalent (Forgejo's `auto_init`); no `--source`/`--push`/`--template` yet | Forgejo `CreateRepoOption` fields only |
| `repo clone` | Uses the host's configured `git_protocol`; private HTTPS clones need Git credentials configured outside fjo; no `gh auth setup-git` yet | fjo does not manage Git credential helpers |
| `repo fork` | `--clone` adds the source as `upstream`; no `--remote` mode | Forgejo fork API |
| `repo edit` | Only description, homepage, visibility (public/private) and default branch; no topics/template/feature toggles | Forgejo `EditRepoOption` subset |
| `repo rename/delete/archive` | Require `--yes` without a terminal. `delete` asks for the repository name like gh; `rename`/`archive` ask y/N | Prevent accidental destructive actions (§5.3) |
| `repo rename` | Does not update the local git remote URL (gh updates it) | BACKLOG B-39 |
| `repo sync` | No `--force`; Forgejo syncs the default branch or one named branch | No force option in the Forgejo API |
| `api` absolute URLs | Only URLs inside the configured instance are accepted | Credentials must not leave the instance (§7.4) |

## Issue command differences

- `issue close --reason` returns exit 15: Forgejo has no close-reason field.
- Labels match any selected label, as Forgejo specifies. Unknown labels and
  milestones fail explicitly. Issue search uses the server index, which is
  updated asynchronously. No GitHub search-qualifier translation is attempted.
- Milestone `number` in JSON is the Forgejo milestone ID; Forgejo has no
  repository-local milestone number.
- `view --comments` is text-only; JSON `comments` remains a count.
- `create --web` prefills title/body; metadata flags return exit 15.
- Missing creation title/body and comment body use simple text prompts;
  there is no interactive metadata picker. Deletion confirmation requires
  the exact issue number, or `--yes` without a terminal.
- Assignee changes use `issue edit --add-assignee/--remove-assignee`; the
  separate `issue assign` convenience command is outside the Phase 3b checklist.

## Pull request command differences

- `pr list` filters merged, draft and assignee matches on the client. Closed
  selects unmerged closed PRs. Draft detection combines Forgejo's `draft` field
  with the case-insensitive `WIP:` and `[WIP]` title prefixes.
- Forgejo 17 supports server-side `base` and `head` parameters. Forgejo 15
  filters branches on the client. `OWNER:BRANCH` also matches the head owner
  on the client. The result limit counts matching PRs across pages.
- JSON `headRepository` is the repository's `owner/name` string. JSON
  `comments` is a count and `reviewers` contains requested user reviewers.
  Diff sizes come from Forgejo's `additions`, `deletions` and `changed_files`
  response fields.
- `pr checks` uses the latest Forgejo status per context for the head SHA in
  the target repository. Success maps to `pass`, failure, error and warning
  map to `fail`, and skipped maps to `skipping`. Pending checks use
  `pending` and leave `completedAt` null. Polling is tracked in B-45.
  A head commit without statuses exits 1 like gh (B-48).
- Branch resolution (`pr view <branch>`, `pr diff`, `pr checks`, `pr review`
  and no argument) only considers open PRs. gh falls back to the most recent
  closed or merged PR for the branch (B-47).

- Forgejo drafts use the `WIP:` title prefix for creation and readiness changes.
- Reviewer requests use a separate Forgejo endpoint. Creation prints the
  new PR URL even if a subsequent reviewer request fails.
- Interactive merge selection uses the repository's enabled Forgejo methods.
  Automated merges and remote head deletion follow Forgejo's repository rules.
  Local cleanup after an immediate merge uses Git's safe branch deletion.
- `pr review` submits one review without inline comments. gh's interactive
  editor flow is reduced to a type selection and a one-line body prompt;
  use `--editor` or `--body-file` for longer reviews. Support for the review
  API is probed before submitting (exit 15 if missing). Self-reviews that
  Forgejo rejects return exit 13 with the server message.

## Release command differences

- Forgejo uses the release tag on the command line. The JSON `id` field
  contains Forgejo's numeric release ID.
- `release view` without a tag uses Forgejo's latest stable release.
  Explicit tag lookup can also find visible drafts through the release list
  when Forgejo's direct tag endpoint returns 404.
- JSON fields are `assets`, `author`, `body`, `createdAt`, `id`,
  `isDraft`, `isPrerelease`, `name`, `publishedAt`, `tagName`,
  `tarballUrl`, `targetCommitish`, `url` and `zipballUrl`.
  `author` contains `login`, and each asset contains `name`, `size`
  in bytes, `url`, `downloadCount` and `createdAt`.
  `assets` is an empty array when there are no attachments. `publishedAt`
  is null for drafts and releases without a publication date.

- Forgejo creates a missing Git tag when a release is published.
  `--verify-tag` requires it to exist before release creation and
  `--cleanup-tag` requests remote Git tag deletion after release deletion.
- Forgejo 15 and 17 leave the existing title or body in place when a release
  edit sends an empty string. fjo reports exit 15 for an attempt to clear
  existing text.
- Forgejo has no equivalent for `--generate-notes`, `--notes-start-tag`,
  `--discussion-category`, `--latest` or `--fail-on-no-commits`.
  `--hide-archive-links` is specific to Forgejo.
