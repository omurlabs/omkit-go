# omur-go-sdk changelog

The Go SDK uses the repo's commit SHA as its "version"; it is vendored by
services through the workspace go.work file rather than released to a
registry. This changelog tracks meaningful API additions/changes.

## 2026-04-17 — Plan 1 SDK consolidation

### Added

- `dbpool.NewPool(ctx, Config{DSN, Role, MaxConns})` — pgxpool wrapper that
  runs `SET ROLE` on every new connection via `AfterConnect`. Defence-in-depth
  complement to `dbpool.WithTenant` for the post-PgBouncer stack. Legacy
  `dbpool.New(ctx, dsn)` preserved for existing callers and marked deprecated.
- `sessions` package — `Session` struct, `Store` interface,
  `NewPostgresStore`, `NewRedisStore`, `BackendFromEnv()`, `New()` factory
  driven by `OMUR_SESSION_BACKEND` (default postgres). Includes
  `ErrNotFound`.
- `eventbus` package — `Event`, `Bus` interface, `NewPostgresBus` (polling),
  `NewRedisBus` (wraps `valkeystream`), `BackendFromEnv()`, `New()` factory
  driven by `OMUR_EVENTBUS_BACKEND` (default postgres).

### Changed

- `settings.Manager` gains `NewFromConfig(Config{Pool, TenantID, Service,
  CachePath, ValkeyAddr, PollInterval})`, `Stop()`, `WithPollInterval(d)`.
  Default backend is `postgres` (polling); opt back in to Valkey via
  `OMUR_SETTINGS_BACKEND=redis` or by passing `WithValkey(...)` to the
  legacy `New`.
- `registry.Registry.New` gains `opts ...Option` with
  `WithPollInterval(d)` and `WithBackend(b)`. Postgres backend reconciles
  via `runPollingLoop` every `pollInterval` (default 10s); Valkey pub/sub
  path preserved for `OMUR_PROVIDERS_BACKEND=redis`.
