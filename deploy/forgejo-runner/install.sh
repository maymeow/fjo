#!/usr/bin/env bash
# Install and register the fjo Linux CI runner (label fjo-linux-amd64:host).
#
# Run as root on the runner server (Debian/Ubuntu with Docker already
# installed):
#
#   RUNNER_VERSION=<x.y.z> \
#   FORGEJO_URL=https://forgejo.example.org \
#   RUNNER_TOKEN=<registration token> \
#   ./install.sh
#
# RUNNER_VERSION: a release from https://code.forgejo.org/forgejo/runner/releases
# RUNNER_TOKEN:   repository/org/instance Settings → Actions → Runners → Create new runner
# RUNNER_NAME:    optional, defaults to the hostname.
set -euo pipefail

: "${RUNNER_VERSION:?set RUNNER_VERSION (e.g. from code.forgejo.org/forgejo/runner/releases)}"
: "${FORGEJO_URL:?set FORGEJO_URL}"
: "${RUNNER_TOKEN:?set RUNNER_TOKEN}"
RUNNER_NAME="${RUNNER_NAME:-$(hostname)}"
RUNNER_USER=forgejo-runner
RUNNER_HOME=/var/lib/forgejo-runner
HERE="$(cd "$(dirname "$0")" && pwd)"

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root" >&2
  exit 1
fi

echo "==> Packages"
apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl git bash gcc libc6-dev make nodejs
if ! command -v docker >/dev/null; then
  echo "Docker is required (integration tests). Install Docker Engine first." >&2
  exit 1
fi
node_major="$(node -p 'process.versions.node.split(".")[0]')"
if [ "$node_major" -lt 20 ]; then
  echo "node >= 20 is required for checkout@v4 / setup-go@v5 (have $node_major)" >&2
  exit 1
fi

echo "==> forgejo-runner $RUNNER_VERSION"
bin="forgejo-runner-${RUNNER_VERSION}-linux-amd64"
base="https://code.forgejo.org/forgejo/runner/releases/download/v${RUNNER_VERSION}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$bin" "$base/$bin"
curl -fsSL -o "$tmp/$bin.sha256" "$base/$bin.sha256"
(cd "$tmp" && sha256sum -c "$bin.sha256")
install -m 0755 "$tmp/$bin" /usr/local/bin/forgejo-runner
forgejo-runner --version

echo "==> User and directories"
if ! id "$RUNNER_USER" >/dev/null 2>&1; then
  useradd --system --create-home --home-dir "$RUNNER_HOME" \
    --shell /usr/sbin/nologin "$RUNNER_USER"
fi
usermod -aG docker "$RUNNER_USER"
install -d -o "$RUNNER_USER" -g "$RUNNER_USER" -m 0750 \
  "$RUNNER_HOME" "$RUNNER_HOME/work" "$RUNNER_HOME/cache" "$RUNNER_HOME/go"
install -d -m 0755 /etc/forgejo-runner
install -m 0644 "$HERE/config.yml" /etc/forgejo-runner/config.yml

echo "==> Register"
if [ -f "$RUNNER_HOME/.runner" ]; then
  echo "already registered ($RUNNER_HOME/.runner exists), skipping"
else
  (cd "$RUNNER_HOME" && runuser -u "$RUNNER_USER" -- \
    forgejo-runner register --no-interactive \
      --config /etc/forgejo-runner/config.yml \
      --instance "$FORGEJO_URL" \
      --token "$RUNNER_TOKEN" \
      --name "$RUNNER_NAME" \
      --labels "fjo-linux-amd64:host")
  chmod 0600 "$RUNNER_HOME/.runner"
fi

echo "==> systemd"
install -m 0644 "$HERE/forgejo-runner.service" /etc/systemd/system/forgejo-runner.service
systemctl daemon-reload
systemctl enable --now forgejo-runner
systemctl --no-pager status forgejo-runner
