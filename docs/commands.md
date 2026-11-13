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
