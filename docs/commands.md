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

Non-TTY output is tab-separated: `full_name`, description (one line), info
(`private`, `fork`, `archived`, `mirror`), updated time (RFC 3339).

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

Delete a repository and everything in it. Requires confirmation; `--yes` skips
the prompt and is required without a terminal. Accepts `-R/--repo`.

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
