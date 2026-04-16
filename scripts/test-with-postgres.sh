#!/usr/bin/env bash
# Run `go test` for the Go SDK against the running docker-compose Postgres and
# Valkey. Passes through any args to `go test` (e.g. `./dbpool/...`,
# `-run TestName`).
set -euo pipefail

cd "$(dirname "$0")/.."

PG_PASS="${POSTGRES_PASSWORD:-}"
VALKEY_PASS="${VALKEY_PASSWORD:-}"
for env_path in ../../.env ../../../omur-core/.env; do
  if [[ -f "$env_path" ]]; then
    if [[ -z "$PG_PASS" ]]; then
      PG_PASS="$(grep -E '^POSTGRES_PASSWORD=' "$env_path" | cut -d= -f2-)"
    fi
    if [[ -z "$VALKEY_PASS" ]]; then
      VALKEY_PASS="$(grep -E '^VALKEY_PASSWORD=' "$env_path" | cut -d= -f2-)"
    fi
  fi
done
if [[ -z "$PG_PASS" ]]; then
  echo "POSTGRES_PASSWORD not set and not found in ../../.env or ../../../omur-core/.env" >&2
  exit 1
fi

exec docker run --rm \
  --network omur-core_backend \
  --dns 8.8.8.8 \
  -v "$PWD":/app \
  -v "$HOME/go/pkg/mod":/go/pkg/mod \
  -w /app \
  -e TEST_POSTGRES_DSN="postgres://omur:${PG_PASS}@postgres:5432/omur?sslmode=disable" \
  -e TEST_REDIS_ADDR="valkey:6379" \
  -e TEST_REDIS_PASSWORD="${VALKEY_PASS}" \
  -e GOFLAGS="-mod=mod" \
  golang:1.26 \
  go test "$@"
