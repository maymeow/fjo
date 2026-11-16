#!/usr/bin/env bash
# Install the fjo Linux CI runner (label fjo-linux-amd64:host), forgejo-runner v13+.
#
# 1. In Forgejo: repository (or org) Settings → Actions → Runners →
#    Create new runner. Copy the UUID and Token it shows.
# 2. On the runner server, as root (Debian/Ubuntu, Docker already installed):
#
#   RUNNER_VERSION=<x.y.z> \
#   FORGEJO_URL=https://forgejo.example.org \
#   RUNNER_UUID=<uuid> \
#   RUNNER_TOKEN=<token> \
#   ./install.sh
#
# RUNNER_VERSION: a release from https://code.forgejo.org/forgejo/runner/releases
# FORGEJO_URL:    the instance root URL (as in app.ini ROOT_URL), no repo path.
set -euo pipefail

: "${RUNNER_VERSION:?set RUNNER_VERSION (e.g. from code.forgejo.org/forgejo/runner/releases)}"
: "${FORGEJO_URL:?set FORGEJO_URL}"
: "${RUNNER_UUID:?set RUNNER_UUID (Settings → Actions → Runners → Create new runner)}"
: "${RUNNER_TOKEN:?set RUNNER_TOKEN (Settings → Actions → Runners → Create new runner)}"
FORGEJO_URL="${FORGEJO_URL%/}"
RUNNER_USER=forgejo-runner
RUNNER_HOME=/var/lib/forgejo-runner
CONFIG=/etc/forgejo-runner/config.yml
HERE="$(cd "$(dirname "$0")" && pwd)"

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root" >&2
  exit 1
fi

# Reject values that would break the generated YAML.
for v in FORGEJO_URL RUNNER_UUID RUNNER_TOKEN; do
  case "${!v}" in
    *[[:space:]\"\'\#]*) echo "$v contains whitespace, quotes or #" >&2; exit 1 ;;
  esac
done

echo "==> Check Forgejo Actions endpoint"
# Same RPC the runner uses on start. A working instance answers 200 with a
# JSON body; HTML, a redirect or an empty 404 means a wrong URL, Actions
# disabled ([actions] ENABLED) or a reverse proxy blocking /api/actions/.
ping_url="$FORGEJO_URL/api/actions/ping.v1.PingService/Ping"
code="$(curl -sS -o /tmp/fjo-runner-ping.$$ -w '%{http_code} %{content_type}' \
  -X POST -H 'Content-Type: application/json' -d '{"data":"fjo"}' "$ping_url" || true)"
if [ "${code%% *}" != 200 ] || ! grep -q '"data"' /tmp/fjo-runner-ping.$$; then
  echo "Ping failed: $ping_url -> HTTP $code" >&2
  head -c 300 /tmp/fjo-runner-ping.$$ >&2 || true
  echo >&2
  rm -f /tmp/fjo-runner-ping.$$
  exit 1
fi
rm -f /tmp/fjo-runner-ping.$$
echo "ok"

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
# Leftover from a failed legacy `register` attempt.
rm -f "$RUNNER_HOME/.runner"

echo "==> Config ($CONFIG)"
install -d -m 0755 /etc/forgejo-runner
umask 027
{
  cat "$HERE/config.yml"
  cat <<EOF

server:
  connections:
    forgejo:
      url: "$FORGEJO_URL"
      uuid: "$RUNNER_UUID"
      token: "$RUNNER_TOKEN"
EOF
} > "$CONFIG.new"
chown root:"$RUNNER_USER" "$CONFIG.new"
chmod 0640 "$CONFIG.new"
mv "$CONFIG.new" "$CONFIG"
umask 022

echo "==> systemd"
install -m 0644 "$HERE/forgejo-runner.service" /etc/systemd/system/forgejo-runner.service
systemctl daemon-reload
systemctl enable forgejo-runner
systemctl restart forgejo-runner
sleep 3
systemctl --no-pager status forgejo-runner || true
journalctl -u forgejo-runner -n 20 --no-pager
