# fjo Linux CI runner

Forgejo Actions runner for the `fjo-linux-amd64` label in
[`.forgejo/workflows/ci.yaml`](../../.forgejo/workflows/ci.yaml). It runs the
`test` (linux leg), `integration` and `lint` jobs.

| File | Installed as |
|---|---|
| `config.yml` | `/etc/forgejo-runner/config.yml` |
| `forgejo-runner.service` | `/etc/systemd/system/forgejo-runner.service` |
| `install.sh` | one-shot installer (run as root) |

## Why host mode

`scripts/test-forgejo.sh` starts Forgejo containers with
`-p 127.0.0.1:<port>:3000` and connects to `127.0.0.1:<port>`. Inside a job
container that address is the container itself, so the label is
`fjo-linux-amd64:host` and jobs run directly on the server as the
`forgejo-runner` user.

## Server requirements

- Linux amd64 (Debian/Ubuntu for `install.sh`), Docker Engine installed.
- git, bash, curl, gcc + libc headers (`go test -race` needs cgo).
- Node.js ≥ 20 (`actions/checkout@v4` and `actions/setup-go@v5` are JS actions).
- Outbound HTTPS to the Forgejo instance, `code.forgejo.org` (actions),
  `go.dev`/`proxy.golang.org` (Go toolchain, modules, staticcheck,
  govulncheck) and `codeberg.org` (Forgejo test images).
- Go is **not** needed on the host: setup-go installs the version from `go.mod`.

## Install

1. Create the runner in Forgejo: repository (or org)
   *Settings → Actions → Runners → Create new runner*. Copy the **UUID** and
   **Token** it shows (runner v13+ no longer uses `forgejo-runner register`).
2. Copy this directory to the server and run:

   ```bash
   sudo RUNNER_VERSION=<x.y.z> FORGEJO_URL=https://forgejo.example.org RUNNER_UUID=<uuid> RUNNER_TOKEN=<token> ./install.sh
   ```

   Pick `RUNNER_VERSION` from
   <https://code.forgejo.org/forgejo/runner/releases>. The script first pings
   the Forgejo Actions endpoint, then verifies the binary's SHA-256, creates
   the `forgejo-runner` system user (in the `docker` group), writes the config
   with a `server.connections` entry (mode 0640) and starts the systemd service.

### `Cannot ping the Forgejo instance server … invalid content-type: ""`

The runner reached *something* at `FORGEJO_URL`, but not Forgejo's Actions
API. Test it from the runner server:

```bash
curl -i -X POST -H 'Content-Type: application/json' -d '{"data":"x"}' https://forgejo.example.org/api/actions/ping.v1.PingService/Ping
```

A healthy instance returns `200` with `{"data":"Hello, x!"}`. Common causes:

- `FORGEJO_URL` is not the instance root (a repo/settings path, `http://`
  that redirects to `https://`, or a different host than `ROOT_URL`).
- Actions disabled: `[actions] ENABLED = false` in `app.ini`.
- A reverse proxy or WAF blocks or rewrites `POST /api/actions/…`
  (it must pass the request through unchanged, including `Content-Type`).
3. Check the runner is *Idle* in the Forgejo UI, then push a commit or re-run
   the `ci` workflow.

Logs: `journalctl -u forgejo-runner -f`.

## Security

- Membership in `docker` is root-equivalent on the server, and host-mode jobs
  run arbitrary workflow code there. Use a dedicated machine/VM with no
  production credentials or access.
- Enable *require approval for workflows from forks* in the repository settings
  (see the comment in `ci.yaml`).
- Register at repository or organisation scope, not instance scope, so other
  repositories cannot schedule jobs on this host.
- Job workdirs live under `/var/lib/forgejo-runner/work` and are removed after
  each job; Go module/build caches in `/var/lib/forgejo-runner/{go,.cache}` are
  kept for speed. Wipe them if a job could have poisoned them.

## Windows / macOS

`fjo-windows-amd64` and `fjo-macos-arm64` need their own runners on those
platforms (also host mode). Reuse `config.yml` with the label changed to
`fjo-windows-amd64:host` / `fjo-macos-arm64:host`, the `envs` paths adjusted,
and the platform requirements from [docs/development.md](../../docs/development.md#ci-runners).
