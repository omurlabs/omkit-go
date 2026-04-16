#!/usr/bin/env bash
# Run `go test` for the Go SDK against the running docker-compose Postgres.
# Passes through any args to `go test` (e.g. `./dbpool/...`, `-run TestName`).
set -euo pipefail

cd "$(dirname "$0")/.."

PG_PASS="${POSTGRES_PASSWORD:-}"
if [[ -z "$PG_PASS" && -f ../../.env ]]; then
  PG_PASS="$(grep -E '^POSTGRES_PASSWORD=' ../../.env | cut -d= -f2-)"
fi
if [[ -z "$PG_PASS" ]]; then
  echo "POSTGRES_PASSWORD not set and not found in ../../.env" >&2
  exit 1
fi

exec docker run --rm \
  --network omur-core_backend \
  --dns 8.8.8.8 \
  -v "$PWD":/app \
  -v "$HOME/go/pkg/mod":/go/pkg/mod \
  -w /app \
  -e TEST_POSTGRES_DSN="postgres://omur:${PG_PASS}@postgres:5432/omur?sslmode=disable" \
  -e GOFLAGS="-mod=mod" \
  golang:1.26 \
  go test "$@"
