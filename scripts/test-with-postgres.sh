#!/usr/bin/env bash
# Run `go test` for the Go SDK against a Postgres instance.
#
# If TEST_POSTGRES_DSN is already exported, it is used directly and no
# container is started. Otherwise a self-contained ephemeral Postgres
# container is spun up on a random host port, used for the test run, and
# removed on exit.
#
# Any args are passed through to `go test` (e.g. `./dbpool/...`,
# `-run TestName`).
set -euo pipefail

cd "$(dirname "$0")/.."

# Fast path: caller already provided a DSN — no Docker needed.
if [[ -n "${TEST_POSTGRES_DSN:-}" ]]; then
  exec go test "$@"
fi

command -v docker >/dev/null 2>&1 || {
  echo "docker not found and TEST_POSTGRES_DSN not set" >&2
  exit 1
}

PG_NAME="omkit-test-pg-$$"
PG_PASS="omkit-test-$$"
PG_USER="omkit"
PG_DB="omkit"
PG_IMAGE="${TEST_POSTGRES_IMAGE:-postgres:16-alpine}"

cleanup() {
  docker rm -f "$PG_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# -p 0:5432 -> ephemeral host port assigned by Docker.
docker run -d --rm \
  --name "$PG_NAME" \
  -e POSTGRES_PASSWORD="$PG_PASS" \
  -e POSTGRES_USER="$PG_USER" \
  -e POSTGRES_DB="$PG_DB" \
  -p 0:5432 \
  "$PG_IMAGE" >/dev/null

# Resolve the host-side ephemeral port.
PG_PORT="$(docker port "$PG_NAME" 5432/tcp | head -n1 | awk -F: '{print $NF}')"
if [[ -z "$PG_PORT" ]]; then
  echo "failed to resolve ephemeral postgres port" >&2
  exit 1
fi

# Wait for readiness (pg_isready inside the container).
for _ in $(seq 1 60); do
  if docker exec "$PG_NAME" pg_isready -U "$PG_USER" -d "$PG_DB" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
if ! docker exec "$PG_NAME" pg_isready -U "$PG_USER" -d "$PG_DB" >/dev/null 2>&1; then
  echo "postgres did not become ready in time" >&2
  exit 1
fi

export TEST_POSTGRES_DSN="postgres://${PG_USER}:${PG_PASS}@127.0.0.1:${PG_PORT}/${PG_DB}?sslmode=disable"

go test "$@"
