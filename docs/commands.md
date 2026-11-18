# Command reference

> Will be generated from the cobra command tree (IMPLEMENTATION §14).

## Global flags

| Flag | Description |
|---|---|
| `--color auto\|always\|never` | Use color in output |
| `--verbose` | Show progress details on stderr |
| `--debug` | Show sanitized diagnostic details on stderr (also `FJO_DEBUG=1`) |
| `-h, --help` | Show help |
| `-v, --version` | Show version |

## Output flags

Commands with structured output (`repo list`, `repo view`, the issue and
pull request list/view commands and `pr checks`) add:

| Flag | Description |
|---|---|
| `--json FIELDS` | Output JSON with a comma-separated field list |
| `-q, --jq EXPR` | Filter JSON output using a jq expression |
| `-t, --template TPL` | Format JSON output using a Go template |
| `--no-pager` | Disable the pager for this command |

`--json` requires at least one field. Bare `--json` is a usage error (exit 10)
that lists the available fields, sorted. Unknown fields, and `--jq` or
`--template` without `--json`, are usage errors too. JSON is indented when
stdout is a terminal and compact otherwise; `--json` is never sent through the
pager. As in gh, a `--jq` result that is a string is printed raw, without JSON
quotes (`--jq '.[].title'` prints one title per line); other results are JSON.

Text from the server (descriptions, titles, `--jq` strings) is sanitized
before it is written to a terminal: control characters are shown in caret
notation (`^[` for ESC), so they cannot inject terminal escape sequences.
Tables are sanitized in TSV mode as well; raw `--json` and piped `--jq` output
keep the exact values.

The pager is chosen from `FJO_PAGER`, the `defaults.pager` config value,
`PAGER`, then `less -FRX`. It is not started when stdout is not a terminal,
when `--no-pager` is given, or when the pager executable is missing. The
editor used by `--editor` follows `FJO_EDITOR`, `defaults.editor`, `VISUAL`,
`EDITOR`, then `notepad` on Windows or `nano` elsewhere. `--editor` needs a
terminal; without one it is a usage error. `--body ""` is an explicit empty
body (it conflicts with `--body-file` like any other `--body`).

Templates support `tablerow`, `tablerender`, `timeago`, `color`, `join`,
`pluck` and `truncate`.

## fjo version

Show version information.

```text
$ fjo version
fjo version 0.0.1
commit: abc1234def56
built: 2026-11-13T12:00:00Z
go: go1.27.4
platform: windows/amd64
```

## fjo auth

Manage Forgejo logins. Tokens are stored in the OS credential store, or in
`credentials.yaml` with `--insecure-storage`; they are never written to
`config.yaml`. See [authentication.md](authentication.md) for the full model.

```text
fjo auth login
fjo auth logout
fjo auth status
fjo auth switch
fjo auth token
```

### fjo auth login

```text
fjo auth login [flags]
```

| Flag | Description |
|---|---|
| `-h, --hostname HOST` | Host ID or URL (default: `FJO_HOST`, then a prompt) |
| `--with-token` | Read the token from standard input (required without a terminal) |
| `-w, --web` | Open the token settings page in the browser |
| `-p, --git-protocol ssh\|https` | Protocol for git operations on this host |
| `--insecure-storage` | Store the token in a plain-text file instead of the OS keyring |
| `--insecure-http` | Allow plain HTTP for this host (trusted development hosts only) |

The token is verified with `GET /user` before it is stored. `login` refuses to
run while `FJO_TOKEN` is set.

### fjo auth status

Verify every stored token and print the accounts per host. Exit code 1 if any
token is invalid or a host cannot be reached.

| Flag | Description |
|---|---|
| `-h, --hostname HOST` | Limit the report to one host |
| `-t, --show-token` | Print the tokens (sensitive) |
| `-a, --active` | Show only active accounts |

### fjo auth switch

Change the active account for a host. Without `--user`, the only other account
is used, or a prompt is shown; an ambiguous choice without a terminal fails.

| Flag | Description |
|---|---|
| `-h, --hostname HOST` | Host to switch (default: the only host with several accounts) |
| `-u, --user USER` | Account to switch to |

### fjo auth logout

Delete a stored token and forget the account. Removing the active account
promotes the next one; removing the last account removes the host entry.

| Flag | Description |
|---|---|
| `-h, --hostname HOST` | Host to log out of |
| `-u, --user USER` | Account to log out of |

### fjo auth token

Print the token for a host and account to standard output. This is sensitive;
it exists for scripting and is never included in diagnostic logs.

| Flag | Description |
|---|---|
| `-h, --hostname HOST` | Host whose token to print |
| `-u, --user USER` | Account whose token to print (default: the active account) |

## fjo repo

Work with repositories (IMPLEMENTATION §5.3). Commands that accept
`[<repository>]` (`[HOST/]OWNER/REPO` or a clone URL) use the repository of the
current directory when it is omitted:

1. `-R/--repo` on commands that accept it (view, fork, edit, rename, delete,
   archive, unarchive, sync)
2. `FJO_REPO`
3. the default chosen with `fjo repo set-default`
4. the git remote that points to a known Forgejo host (configured hosts and
   `FJO_HOST`). Remotes on other hosts are ignored; `origin` is not special.
   Several remotes pointing to *different* repositories are an error (exit 10)
   with a hint to use `set-default`.

HTTPS remotes are matched against host IDs including subpath installs
(`https://example.com/forgejo/owner/repo.git`); SSH remotes through
`ssh_hosts` of a host entry, else by hostname (ambiguous hostnames are
reported, not guessed).

### fjo repo view [<repository>]

| Flag | Description |
|---|---|
| `-R, --repo REPO` | Select the repository: `[HOST/]OWNER/REPO` |
| `-w, --web` | Open the repository in the browser (prints the URL when no browser is available) |
| `-b, --branch BRANCH` | Open a branch (with `--web`) |
| `--json FIELDS` | Output JSON with the selected fields |
| `-q, --jq EXPR` | Filter JSON output with jq (requires `--json`) |
| `-t, --template TPL` | Format JSON output with a Go template (requires `--json`) |
| `--no-pager` | Disable the pager |

JSON fields: `name`, `nameWithOwner`, `description`, `isPrivate`,
`isInternal`, `isFork`, `isMirror`, `isTemplate`, `isArchived`, `isEmpty`,
`defaultBranch`, `primaryLanguage`, `topics`, `url`, `cloneUrl`, `sshUrl`,
`websiteUrl`, `stargazerCount`, `forkCount`, `openIssueCount`,
`openPullRequestCount`, `parent`, `createdAt`, `updatedAt`.

On a terminal: name, description, visibility/fork/archived, default branch,
counters and URL. Otherwise stable `key:<TAB>value` lines (`name`,
`description`, `visibility`, `default_branch`, `fork`, `parent`, `mirror`,
`archived`, `stars`, `forks`, `open_issues`, `open_pull_requests`, `url`,
`clone_url`, `ssh_url`).

### fjo repo list [<owner>]

Repositories owned by a user or organization (default: the authenticated
user), most recently updated first.

| Flag | Description |
|---|---|
| `-L, --limit N` | Maximum number of repositories (default 30) |
| `--visibility public\|private` | Filter by visibility |
| `--archived` / `--no-archived` | Only / no archived repositories |
| `--fork` / `--source` | Only forks / only non-forks |
| `--hostname HOST` | Forgejo host (default: `FJO_HOST`, `defaults.host`) |
| `--json FIELDS` | Output JSON with the selected fields |
| `-q, --jq EXPR` | Filter JSON output with jq (requires `--json`) |
| `-t, --template TPL` | Format JSON output with a Go template (requires `--json`) |
| `--no-pager` | Disable the pager |

JSON fields are the same as `repo view --json`. Non-TTY output is
tab-separated: `full_name`, description (one line), info (`private`, `fork`,
`archived`, `mirror`), updated time (RFC 3339).

### fjo repo create <name>

Create a repository for the authenticated user or an organization. `<name>`
may be `OWNER/NAME`; without `--private` or `--public` the server default
visibility is used.

| Flag | Description |
|---|---|
| `--hostname HOST` | Forgejo host (default: `FJO_HOST`, `defaults.host`) |
| `--owner ORG` | Organization that will own the repository |
| `--description TEXT` | Repository description |
| `--private` / `--public` | Visibility (mutually exclusive) |
| `--initialize` | Create an initial commit |
| `--gitignore NAME` | Gitignore template (requires `--initialize`) |
| `--license NAME` | License template (requires `--initialize`) |
| `--default-branch NAME` | Default branch (requires `--initialize`) |
| `--clone` | Clone the new repository after creating it |

The repository URL is printed on stdout. A failed creation changes nothing; a
failed clone reports the created repository and how to clone it manually.

### fjo repo clone <repository> [<directory>] [-- <gitflags>...]

Clone a repository using the installed `git`. The URL is chosen from the
host's `git_protocol` (`ssh` or `https`; see `fjo auth login --git-protocol`).
Arguments after `--` are passed to `git clone` unchanged. The default
directory is the repository name.

Clone URLs and repository names come from the server and are treated as
untrusted: only `https`, `http` and `ssh` URLs are accepted (no options,
`file://`, local paths or `<helper>::` transports), the URL and directory are
passed after `--`, and a server-provided name is used as a directory only if
it is a single safe path element (otherwise pass the directory explicitly).
The same applies to `repo create --clone` and `repo fork --clone`.

### fjo repo fork [<repository>]

Fork a repository into the authenticated user's account or an organization.

| Flag | Description |
|---|---|
| `-R, --repo REPO` | Select the source repository |
| `--org ORG` | Fork into this organization (default: your account) |
| `--name NAME` | Name of the fork (default: the source name) |
| `--clone` | Clone the fork and add the source as the `upstream` remote |

### fjo repo edit [<repository>]

Edit repository properties. Only flags that are passed are changed.

| Flag | Description |
|---|---|
| `-R, --repo REPO` | Select the repository |
| `--description TEXT` | Set the description (empty clears it) |
| `--homepage URL` | Set the homepage URL |
| `--visibility public\|private` | Change visibility |
| `--default-branch NAME` | Change the default branch |

Forgejo's API has no `internal` visibility value, so only public and private
are accepted.

### fjo repo rename <new-name> / rename <repository> <new-name>

Rename a repository. With one argument the repository is resolved normally
(`-R`, `FJO_REPO`, the default, or the only remote); the two-argument form
selects it explicitly. This is a destructive operation and asks for
confirmation; `--yes` skips the prompt (required without a terminal).

### fjo repo delete [<repository>]

Delete a repository and everything in it. On a terminal you must type the
repository name (`OWNER/REPO`, or `HOST/OWNER/REPO`) to confirm; "y" is not
enough. `--yes` skips the prompt and is required without a terminal. Accepts
`-R/--repo`.

### fjo repo archive [<repository>] / unarchive [<repository>]

Archive a repository (read-only) or make it writable again. `archive` asks for
confirmation and supports `--yes`; `unarchive` does not need confirmation.
Both accept `-R/--repo`.

### fjo repo sync [<repository>]

Sync a fork with its upstream repository. Without `--branch` only the default
branch is synced.

| Flag | Description |
|---|---|
| `-R, --repo REPO` | Select the fork |
| `-b, --branch BRANCH` | Sync this branch instead of the default branch |

### fjo repo set-default [<repository>]

Choose the repository used in this clone when several remotes point to
different repositories. Stored as `remote.<name>.fjo-resolved` in the local
`.git/config`. The repository must be one of the remotes; without an argument
the only candidate is taken, or a prompt is shown on a terminal.

| Flag | Description |
|---|---|
| `-v, --view` | Print the current default (`HOST/OWNER/REPO`) |
| `-u, --unset` | Remove the default |

## fjo issue

Work with repository issues. All commands accept `-R/--repo [HOST/]OWNER/REPO`
and otherwise use the same repository selection as `fjo repo`. An issue
argument accepts `42`, `#42` or a full issue URL; the URL selects the host
and repository even when `--repo` or `FJO_REPO` selects something else.

| Command | Flags and behavior |
|---|---|
| `issue list` (`ls`) | `-s/--state open\|closed\|all` (default open), `--author`, `-a/--assignee`, repeatable `-l/--label`, `-m/--milestone`, `-S/--search`, `-L/--limit` (default 30), `-w/--web` |
| `issue view <number\|url>` | `-c/--comments`, `-w/--web`; Markdown bodies are plain text |
| `issue create` | `-t/--title`, `--body`, `--body-file` (`-` reads stdin), `--editor`, `-l/--label`, `-a/--assignee`, `-m/--milestone`, `-w/--web` |
| `issue edit <number\|url>` | `-t/--title`, body flags, `--add-label`, `--remove-label`, `--add-assignee`, `--remove-assignee`, `-m/--milestone`, `--remove-milestone` |
| `issue close <number\|url>` / `reopen` | `-c/--comment` adds a comment before changing state; `close --reason` is unsupported (exit 15) |
| `issue comment <number\|url>` | Body flags; a nonempty body is required |
| `issue delete <number\|url>` | Type the issue number to confirm; `--yes` skips this and is required without a terminal |
| `issue pin <number\|url>` / `unpin` | Supported by both Forgejo 15 and 17 |

`list` and `view` support `--json FIELDS`, `--jq`, `--template` and
`--no-pager`. JSON fields: `number`, `title`, `state`, `author`, `url`,
`body`, `labels`, `milestone`, `assignees`, `comments`, `createdAt`,
`updatedAt`, `closedAt`. The `comments` field is a count; `view --comments`
prints the discussion as text and cannot be combined with `--json`.

Non-TTY lists are TSV: number (with `#`), title, label names, updated time
(RFC 3339). Empty lists print a diagnostic to stderr; JSON is `[]`.
Pagination is automatic and `--limit` is the maximum total item count.
Labels match any requested label, following Forgejo's API. Unknown label or
milestone names fail explicitly. Text search uses Forgejo's index and newly
created issues may take a moment to appear.

Author/assignee filters and assignee mutations accept `@me`. Labels use
names; milestones accept titles or IDs, with an exact title preferred.
Editing retains fields that were not supplied; `--body ""` clears the body,
and `--remove-milestone` clears the milestone. Assignee edits preserve other
assignees. Label changes use separate API requests: if one fails, the error
reports that the issue may be partially updated.

On a terminal, missing creation title/body and comment body are prompted.
Without a terminal, creation requires `--title` and comments require a body
source; no prompt is started. Creation and comments print the resulting URL
to stdout; other mutation diagnostics go to stderr.

`create --web` opens the browser form with title/body prefilled. Metadata
flags are unsupported in this mode (exit 15); select them in the form.
`list --web` carries its filters into the browser URL; `view --web` opens
the issue directly. When no browser is available the URL is printed.

```text
fjo issue list -R org/project --assignee @me --label bug
fjo issue view '#42' --comments
fjo issue create --title "Bug" --body-file issue.md
fjo issue edit 42 --add-label bug --remove-milestone
fjo issue close 42 --comment "Fixed in the next release"
fjo issue list --json number,title --jq '.[].title'
```

## fjo api

Make an authenticated request to the Forgejo REST API (IMPLEMENTATION §5.9).
The fallback for endpoints without a dedicated command.

```text
fjo api <endpoint> [flags]
```

| Flag | Description |
|---|---|
| `--hostname HOST` | Forgejo host (default: `FJO_HOST`, `defaults.host`, the only configured host). No `-h` shorthand, as in gh. |
| `-X, --method METHOD` | HTTP method (default GET, or POST when fields or `--input` are given) |
| `-F, --field key=value` | Typed parameter: `true`, `false`, `null`, integers; `@file` / `@-` read the value from a file / stdin |
| `-f, --raw-field key=value` | String parameter |
| `-H, --header key:value` | Extra request header (repeatable) |
| `--input FILE` | Request body from a file (`-` = stdin); fields then go to the query string |
| `--paginate` | Follow `Link: rel="next"` within the instance and merge JSON arrays |
| `-i, --include` | Print the status line and response headers |
| `--silent` | Do not print the response body |

- `<endpoint>` is relative to the API root (`/user` → `https://host/api/v1/user`)
  or an absolute URL inside the same instance.
- For GET (and HEAD) fields become query parameters; otherwise a JSON object.
  Nested keys: `labels[]=1` (array), `meta[key]=v` (object).
- JSON is indented when stdout is a terminal, printed verbatim otherwise.
- On HTTP status >= 400 the body is printed to stdout, the error to stderr,
  exit code 4/11/12/13/14 (see compatibility.md).
- Authentication is required (exit 4 without a login or `FJO_TOKEN`).
- `{owner}`, `{repo}` and `{branch}` in the endpoint are filled from the
  current repository (see `fjo repo`) and the checked-out branch; the request
  then goes to that repository's host unless `--hostname` is given.

```text
fjo api /user
fjo api /repos/owner/repo/issues --paginate
fjo api /user/repos -F name=demo -F private=true
fjo api -X PATCH /repos/owner/repo -f description="New description"
```

## fjo completion

Generate shell completion scripts (bash, zsh, fish, powershell).

## Pull requests

All pull request commands accept `-R, --repo [HOST/]OWNER/REPO`.

### `fjo pr list`

```bash
fjo pr list --state merged --base main --limit 10 --json number,title,mergedAt
fjo pr list --head alice:feature --assignee @me --label bug --draft
```

`--state` accepts `open` (default), `closed`, `merged` and `all`.
Use `--author` or `--assignee` with a login or `@me`, repeat `--label`
to select labels, and use `--base` and `--head` to filter branches.
`--draft` selects drafts, including titles with a `WIP:` or `[WIP]` prefix.
`-L, --limit` defaults to 30 and counts matches after filtering.
`--web` opens Forgejo's pull request list and supports its state (open,
closed or all), author, assignee and label filters.

### `fjo pr view [<number|url|branch>]`

```bash
fjo pr view 42 --comments
fjo pr view alice:feature --json number,headRefName --jq '.headRefName'
fjo pr view https://git.example.org/team/project/pulls/42 --web
```

Pass a PR number, `#NUMBER`, a Forgejo PR URL or a branch. A URL selects
the instance and repository; a `--repo` naming a different repository is
rejected. Trailing
tabs such as `/files` or `/commits` are accepted. With no argument, fjo looks for an open PR
matching the current branch and head owner. A tracking branch on the same
Forgejo instance supplies the remote branch name and owner. If several PRs
match, specify a number or URL. `--comments` includes discussion as plain
text. `--web` opens the PR in a browser.

### `fjo pr diff [<number|url|branch>]`

```bash
fjo pr diff 42 --name-only
fjo pr diff 42 --patch --color=never > change.patch
```

The default streams a unified diff through the configured pager on a
terminal. `--patch` streams the patch format and `--name-only` lists
changed paths. `--no-pager` disables the pager. The PR is selected as in
`pr view`; with no argument, fjo resolves it from the current branch.

### `fjo pr checks [<number|url|branch>]`

```bash
fjo pr checks 42 --json name,state,bucket,url
```

Shows the latest status in each context for the head commit. Any failure
returns exit 1. Pending checks return exit 8 when there are no failures.
Passing or skipped checks return exit 0. A head commit without any
status returns exit 1 with "no checks reported", like gh; `--json` then
prints `[]`. The PR is
selected as in `pr view`; with no argument, fjo resolves it from the current
branch.

`pr list`, `pr view` and `pr checks` support `--json`, `--jq` and
`--template`. Pass `--json` without a field list to see the available fields.

### `fjo pr create`

```bash
fjo pr create --title "Add search" --body-file pr.md
fjo pr create --base main --fill --draft --push
fjo pr create --head alice:feature --title "Add search" --reviewer bob
```

The head defaults to the current Git branch. A configured push remote or
tracking branch can select a fork and its branch name. The base defaults to
the target repository's default branch. If an open pull request already
matches those branches, the command exits with code 13 and its URL.

`--fill` fills the title and body from commits in the base-to-head range.
`--fill-first` uses only the first commit in that range. Explicit
`--title` and body flags override commit-derived text. Without a terminal,
provide `--title`, `--fill` or `--fill-first`.

Use `--push` to publish a missing head branch. In a terminal, fjo asks
before pushing if the flag is absent. `--draft` sets the `WIP:` title
prefix. Support for metadata includes `--label`, `--assignee`,
`--reviewer` and `--milestone`. `--web` opens the comparison on Forgejo
so creation can be completed in the browser.

### `fjo pr checkout <number|url>`

```bash
fjo pr checkout 42 --branch review-42
```

Fetches the pull request head via a matching Git remote. The local branch
defaults to the PR head name. Existing local branches must be able to
fast-forward. `--force` allows replacing a diverged branch. Checkout rejects
a dirty working tree in both modes. For a fork, checkout sets an upstream
when the fork's remote-tracking branch is available locally.

### `fjo pr merge [<number|url|branch>]`

```bash
fjo pr merge 42 --squash --delete-branch
fjo pr merge 42 --merge --auto
```

Select a repository-enabled method with `--merge`, `--squash`, `--rebase`,
`--rebase-merge` or `--fast-forward-only`. With a terminal, fjo offers the
enabled methods when none is specified. `--auto` asks Forgejo to merge
when checks succeed. Use `--subject` and the body flags for the merge
message. `--delete-branch` requests deletion of the remote head branch.
After an immediate merge, local deletion requires a matching branch tip
and Git's safe branch deletion checks.

### `fjo pr review [<number|url|branch>]`

```bash
fjo pr review 42 --approve
fjo pr review 42 --request-changes --body "Please add tests"
fjo pr review --comment --body-file review.md
```

Choose one of `-a, --approve`, `-r, --request-changes` or `-c, --comment`.
`--request-changes` and `--comment` need a nonempty body from `--body`,
`--body-file` (use `-` for standard input) or `--editor`; an approval body
is optional. In a terminal, fjo asks for a missing review type and body.
Without a terminal, the type flag and the required body are mandatory and
no prompt starts. The review is attached to the head commit fjo read.

Before submitting, fjo checks that the server exposes the review API and
exits with code 15 if it does not. Forgejo rejects approving or requesting
changes on your own pull request; fjo reports the server message with
exit code 13. The PR is selected as in `pr view`.

### `fjo pr close`, `reopen`, `ready`, `edit`, `comment`

```bash
fjo pr close 42 --comment "Superseded" --delete-branch
fjo pr reopen 42
fjo pr ready 42 --undo
fjo pr edit 42 --base main --add-label bug --remove-assignee alice --add-reviewer bob
fjo pr comment 42 --body-file notes.md
```

`ready` removes the WIP title prefix. `ready --undo` adds it.
`edit` supports `--title`, `--base`, body flags, label, assignee and
reviewer additions and removals, and `--milestone` or `--remove-milestone`.
Body flags are `--body`, `--body-file` (use `-` for standard input) and
`--editor`.
