# Authentication

Implemented in Phase 1 (v0.0.2). fjo uses Forgejo **personal access tokens
(PATs)**. There is no OAuth2 and no `auth refresh` yet (BACKLOG B-16).

## Creating a token

Create a token in the Forgejo web UI under **Settings → Applications → Generate
New Token**. The minimum scope for logging in is `read:user` (or `user:read`);
add scopes for the commands you intend to use, for example `write:repository`,
`write:issue`, `write:user`.

`fjo auth login --web` opens the token settings page of the selected host.

## Logging in

```bash
# interactive: prompts for the host (unless given) and the token
fjo auth login

# non-interactive: token from standard input
fjo auth login --hostname codeberg.org --with-token < token.txt

# a self-hosted instance under a subpath, or plain HTTP for a dev server
fjo auth login --hostname https://example.com/forgejo --with-token < token.txt
fjo auth login --hostname http://localhost:3000 --insecure-http --with-token < token.txt

# store in a plain-text file where no OS keyring exists
fjo auth login --hostname build.example.com --insecure-storage --with-token < token.txt
```

`auth login` flags:

| Flag | Meaning |
|---|---|
| `-h, --hostname HOST` | Host ID or URL (also `FJO_HOST`); hidden `--host` alias |
| `--with-token` | Read the token from standard input (required without a terminal) |
| `-w, --web` | Open the token settings page in the browser |
| `-p, --git-protocol ssh\|https` | Protocol recorded for git operations on this host |
| `--insecure-storage` | Store the token in `credentials.yaml` instead of the OS keyring |
| `--insecure-http` | Allow plain HTTP for this host (trusted development hosts only) |

The token is verified with `GET /user` before anything is stored; the username
comes from the server and is never typed by the user. The server version is
shown when available. Re-running `login` for the same host with another account
adds that account and makes it active.

The token is never accepted as a command-line argument.

## Where tokens are stored

- **OS credential store (default):** Windows Credential Manager, macOS
  Keychain, Linux Secret Service. The entry is keyed by service
  `fjo:<host ID>` and account `<username>`.
- **Plain-text file (explicit opt-in):** `credentials.yaml` next to
  `config.yaml` in the fjo configuration directory, mode `0600`, with a warning
  header. It is used only with `--insecure-storage`; fjo never silently falls
  back to it. Use it on headless Linux, in containers and in CI where no
  Secret Service is available.
- **`FJO_TOKEN` (environment):** overrides stored credentials for the host the
  command targets (selected by `--hostname`, `FJO_HOST` or `defaults.host`).
  It is never written to disk. `auth login` refuses to run while `FJO_TOKEN` is
  set, because the environment token would win anyway.

`config.yaml` never contains tokens. `fjo auth status` shows the source of each
token (`keyring`, `insecure storage` or `FJO_TOKEN`).

## Multiple hosts and accounts

An account is the pair **(host ID, server username)**; there are no user-defined
aliases. Each host has one active account.

```bash
fjo auth status                       # verify every stored token
fjo auth status --hostname git.example.com --active
fjo auth switch --hostname git.example.com --user deploy-bot
fjo auth logout --hostname git.example.com --user developer
fjo auth token --hostname git.example.com --user deploy-bot
```

- `status` prints one block per host, masks tokens and exits 1 if any token is
  invalid or a host is unreachable. `-t/--show-token` reveals the token
  (sensitive); `-a/--active` shows only active accounts.
- `switch` changes the active account. Without `--user` it picks the only other
  account, or prompts; an ambiguous choice without a terminal is a usage error.
- `logout` deletes the token from both stores. Removing the active account
  promotes the next one; removing the last account removes the host entry and
  clears `defaults.host` if it pointed there.
- `token` prints the token to standard output. It exists for scripting (for
  example `git -c http.extraHeader=...`); never paste its output into logs or
  issue reports.
- `--hostname` accepts a host ID or URL; the canonical host ID is used as the
  config and credential key. A hidden `--host` alias is accepted for older
  examples.

## Host identity and HTTP security

Host IDs follow IMPLEMENTATION §6.2: lowercase host, default port omitted,
scheme omitted (HTTPS implied), base path kept for subpath installs. Plain HTTP
is allowed only when the host entry has `insecure_http: true` (set by
`--insecure-http`).

- TLS verification is always on; there is no general `--insecure` switch.
- A custom CA can be configured per host with `ca_file` in `config.yaml` (PEM
  bundle added to the system roots).
- The `Authorization: token <PAT>` header is sent only to requests inside the
  instance root. It is stripped on redirects to other origins and HTTPS → HTTP
  redirects are refused.
- Retries apply only to safe requests (`GET`, `HEAD`, `OPTIONS`) on
  429/502/503/504 and transient network errors. Tokens and authorization
  headers are redacted from diagnostics and error messages.

## Testing

Unit tests use `httptest` TLS servers and in-memory credential stores. The
opt-in integration suite logs in to real Forgejo 15.0.11 and 17.0.1 containers:

```bash
make test-integration
```

See `docs/development.md` for targeting an existing instance with
`FJO_TEST_FORGEJO_URL` and `FJO_TEST_FORGEJO_TOKEN`.
