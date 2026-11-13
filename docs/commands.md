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

```text
fjo api /user
fjo api /repos/owner/repo/issues --paginate
fjo api /user/repos -F name=demo -F private=true
fjo api -X PATCH /repos/owner/repo -f description="New description"
```

## fjo completion

Generate shell completion scripts (bash, zsh, fish, powershell).
