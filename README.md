# omkit-go

Multi-tenant SaaS scaffolding for Go services.

`omkit-go` is the Go side of Omur Labs' shared service toolkit. It bundles the
boring-but-load-bearing pieces every tenant-isolated service needs: a pgx pool
that enforces Row-Level Security per connection, a pluggable event bus
(Postgres `LISTEN/NOTIFY` or Valkey streams/pubsub), session and settings
stores, an LLM provider abstraction, BYOK encryption + KMS, request-ID
propagation, an Asynq-based job queue with cron scheduling, and a job-queue
envelope contract that interoperates with the Python sibling.

- **Status:** `v0.1.0` — internal API stable.
- **Go:** `1.26.3` (`go.mod`)
- **License:** Apache-2.0
- **Sibling:** [`omkit-python`](https://github.com/omurlabs/omkit-python) — same primitives, same envelope contract, same RLS conventions.

## Install

```bash
go get github.com/omurlabs/omkit-go@v0.1.0
```

Import the packages you actually use; nothing pulls the entire surface.

```go
import (
    "github.com/omurlabs/omkit-go/dbpool"
    "github.com/omurlabs/omkit-go/tenant"
    "github.com/omurlabs/omkit-go/jobqueue"
)
```

## Quickstart

A minimal tenant-aware service wired up with the pieces omkit-go gives you:

```go
package main

import (
    "context"
    "net/http"

    "github.com/omurlabs/omkit-go/config"
    "github.com/omurlabs/omkit-go/dbpool"
    "github.com/omurlabs/omkit-go/health"
    "github.com/omurlabs/omkit-go/logging"
    "github.com/omurlabs/omkit-go/metrics"
    "github.com/omurlabs/omkit-go/requestid"
    "github.com/omurlabs/omkit-go/tenant"
    "github.com/omurlabs/omkit-go/tracing"
)

func main() {
    ctx := context.Background()
    cfg := config.Load()
    logging.Init(cfg)
    _ = tracing.Init(ctx, "my-service")

    pool, _ := dbpool.NewPool(ctx, dbpool.Config{DSN: cfg.PostgresDSN()})
    defer pool.Close()

    mux := http.NewServeMux()
    health.Mount(mux, pool)
    mux.Handle("/metrics", metrics.Handler())

    handler := tenant.Middleware(
        requestid.WithRequestIDPropagation(
            metrics.HTTPMiddleware(mux),
        ),
    )
    _ = http.ListenAndServe(":8080", handler)
}
```

Tenant context flows through `tenant.Middleware`; downstream packages
(`dbpool.WithTenant`, `jobqueue.Client`, `httpclient`, `logging`, `settings`)
all read from `context.Context`.

## Package index

### Auth & identity

| Package         | What it does                                                                |
|-----------------|-----------------------------------------------------------------------------|
| `auth`          | Role-based access control + audit entries (`HasRole`, `RequireRole`, `WriteAuditEntry`). |
| `featureflags`  | Feature-flag store (`PostgresStore`, `StaticStore`) with role-gated `Allowed`. |
| `middleware`    | Admin / bearer-token middleware + role extraction from headers.             |
| `sessions`      | Session `Store` interface with Postgres and Redis backends.                 |
| `tenant`        | Multi-tenant context, RLS resolver, `Middleware`, `UIDCache`.               |

### Data & persistence

| Package    | What it does                                                                       |
|------------|------------------------------------------------------------------------------------|
| `dbpool`   | pgx pool that sets a Postgres role per connection (`WithTenant`, `WithPrivilegedRole`, advisory locks). |
| `quota`    | Per-tenant resource limits (`CheckUpload`, `CheckQuery`, `Decision`).              |
| `cleanup`  | Coordinated periodic task `Loop` with advisory locking — runs exactly one cleaner at a time. |

### Encryption & secrets

| Package      | What it does                                                                |
|--------------|-----------------------------------------------------------------------------|
| `crypto`     | AES-256-GCM envelope encryption, `KUser` session keys, AAD constants (`AADMeta`, `AADMetrics`, `AADContent`, `AADEmbeddingsChunks`). |
| `encryption` | Fernet-compatible encryption for settings secrets — interoperable with `omkit-python`'s `omkit.encryption`. |
| `kms`        | Ops-held key-wrapping interface (`KMS`) + `LocalDevKMS` for dev/tests.      |
| `security`   | Security event logging (`WriteSecurityEvent`) for RAG classifier observations. |

### Configuration & settings

| Package    | What it does                                                                |
|------------|-----------------------------------------------------------------------------|
| `config`   | Env-based base config (`Load`, `PostgresDSN`, `ValkeyAddr`, typed `EnvStr/Int/Bool/Int64`). |
| `settings` | Tenant `Manager` with Valkey polling + local cache; `OnChange` callbacks.   |

### Observability

| Package      | What it does                                                              |
|--------------|---------------------------------------------------------------------------|
| `logging`    | `slog` JSON/text logger with request-ID propagation.                      |
| `metrics`    | Prometheus registry, HTTP `Handler`, `HTTPMiddleware`, helpers for counters/histograms/gauges. |
| `tracing`    | OpenTelemetry init over OTLP/HTTP with the W3C propagator.                |
| `requestid`  | `X-Request-ID` correlation across hops (`HeaderName`, `WithRequestIDPropagation`). |

### HTTP & networking

| Package      | What it does                                                                |
|--------------|-----------------------------------------------------------------------------|
| `httpclient` | HTTP client with retries, bearer/service tokens, tenant header injection, `CircuitBreaker`, OTel tracing. `PostJSON`/`GetJSON` for the common path. |

### Async, eventing, scheduling

| Package        | What it does                                                                |
|----------------|-----------------------------------------------------------------------------|
| `jobqueue`     | Asynq client that propagates tenant + request ID through the task envelope. Same wire contract as `omkit.jobqueue.Envelope` in Python. |
| `scheduler`    | Asynq scheduler with DB-driven reconcile loop for crons (`ProviderSource`, `PgxProviderSource`). |
| `eventbus`     | Pluggable `Bus` — `NewPostgresBus` (`LISTEN/NOTIFY` polling) or `NewRedisBus`. `BackendFromEnv` picks via env. |
| `valkeystream` | Valkey/Redis `XREAD`/`XACK` stream wrapper (`Add`, `Read`, `Ack`, `Len`).   |
| `valkeysub`    | Valkey pub/sub `Subscriber` with auto-reconnect.                            |
| `syncnotifier` | Lightweight HTTP `Notifier` for external sync events.                       |

### LLM provider abstraction

| Package    | What it does                                                                |
|------------|-----------------------------------------------------------------------------|
| `provider` | LLM provider implementations — `AnthropicProvider`, `OpenAIProvider`, `OllamaProvider` behind a shared interface. |

### Lifecycle & health

| Package  | What it does                                                                |
|----------|-----------------------------------------------------------------------------|
| `health` | Liveness + readiness handlers (`Handler`, `ReadyHandlerWithProbes`, `Mount`, `LegacyHealthcheck`). |
| `cost`   | `RecordCost` — per-service, per-provider Prometheus counter for usage cost. |

### Placeholder

| Package    | What it does                                          |
|------------|-------------------------------------------------------|
| `personas` | Reserved namespace; no exports yet.                   |

## Cross-SDK job envelope

`jobqueue` uses the same envelope wire contract as
`omkit.jobqueue.Envelope` in `omkit-python`. A Go Asynq worker and a Python
streaq worker can publish and consume each other's jobs as long as they share
a Valkey/Redis broker and agree on envelope version + tenant routing.

## Stack

Built on a small, opinionated set of dependencies:

- `github.com/jackc/pgx/v5` — Postgres driver and pool
- `github.com/redis/go-redis/v9` — Valkey / Redis client
- `github.com/hibiken/asynq` — job queue + scheduler
- `github.com/prometheus/client_golang` — metrics
- `go.opentelemetry.io/otel` (+ OTLP/HTTP exporter) — tracing
- `log/slog` — structured logging (stdlib)
- `github.com/google/uuid`, `golang.org/x/sync` — primitives

See `go.mod` for exact versions.

## Development

```bash
git clone git@github.com:omurlabs/omkit-go.git
cd omkit-go
go test ./...

# Integration tests against a real Postgres
./scripts/test-with-postgres.sh
```
