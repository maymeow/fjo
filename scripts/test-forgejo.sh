#!/usr/bin/env bash
# Run fjo's integration tests against real Forgejo 15 and 17 containers.
#
# Requires Docker and network access to codeberg.org (first run pulls images).
# Usage:
#   ./scripts/test-forgejo.sh
#   FJO_TEST_FORGEJO_VERSIONS="15.0.11" ./scripts/test-forgejo.sh
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE="${FJO_TEST_FORGEJO_IMAGE:-codeberg.org/forgejo/forgejo}"
VERSIONS="${FJO_TEST_FORGEJO_VERSIONS:-15.0.11 17.0.1}"
USERNAME="fjo-test"
PASSWORD="Fjo-Test-Passw0rd!"

containers=()

cleanup() {
  for name in "${containers[@]}"; do
    docker rm -f "$name" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

for version in $VERSIONS; do
  name="fjo-forgejo-${version//./-}"
  containers+=("$name")

  echo "==> Forgejo $version"
  docker run -d --name "$name" -p 127.0.0.1::3000 \
    -e FORGEJO__security__INSTALL_LOCK=true \
    -e FORGEJO__database__DB_TYPE=sqlite3 \
    -e FORGEJO__service__DISABLE_REGISTRATION=true \
    "$IMAGE:$version" >/dev/null

  port="$(docker port "$name" 3000/tcp | head -n1 | sed 's/.*://')"
  if [ -z "$port" ]; then
    echo "cannot determine the published port for $name" >&2
    exit 1
  fi
  url="http://127.0.0.1:$port"

  ready=false
  for _ in $(seq 1 60); do
    if curl -fsS "$url/api/v1/version" >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done
  if [ "$ready" != true ]; then
    echo "Forgejo $version did not become ready" >&2
    docker logs "$name" >&2 || true
    exit 1
  fi

  docker exec -u git "$name" forgejo admin user create \
    --admin --username "$USERNAME" --password "$PASSWORD" \
    --email "$USERNAME@example.com" --must-change-password=false >/dev/null

  token="$(docker exec -u git "$name" forgejo admin user generate-access-token \
    --username "$USERNAME" --token-name fjo-integration --scopes all --raw)"

  FJO_TEST_FORGEJO_URL="$url" \
  FJO_TEST_FORGEJO_TOKEN="$token" \
  FJO_TEST_FORGEJO_VERSION="$version" \
    go test -tags=integration ./test/integration -count=1 -v

  docker rm -f "$name" >/dev/null
done
