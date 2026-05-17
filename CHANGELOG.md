# omur-go-sdk changelog

The Go SDK uses the repo's commit SHA as its "version"; it is vendored by
services through the workspace go.work file rather than released to a
registry. This changelog tracks meaningful API additions/changes.

## Unreleased

### Changed

- `eventbus/postgres.go` — `Publish()` now stamps the nil-UUID sentinel
  (`NilTenantID = "00000000-0000-0000-0000-000000000000"`) on system-level
  events instead of writing NULL `tenant_id`. Aligns the SDK with the
  RLS policy introduced in migration `0005_rls_with_check_and_events_sentinel`
  so admin-role readers can see system events without re-opening the
  cross-tenant leak the migration closed. Backfill of legacy NULL rows
  ships in migration `0006_events_backfill_nil_tenant`. (#549)

### Added

- `eventbus.NilTenantID` constant — the nil-UUID sentinel reserved for
  system events. Mirrored by `omur_sdk.eventbus.NIL_TENANT_ID` in Python.
- `crypto/` package: `Wrap`/`Unwrap` AES-256-GCM helpers, `KUser` session key
  type, and AAD purpose-string constants (`AADMeta`, `AADMetrics`,
  `AADContent`, `AADEmbeddingsChunks`) extracted from `services/spine/auth`.
  Byte-for-byte compatible with existing ciphertexts (pinned by
  `TestGoldenVectorUnwrap` and `TestLegacyBlobsDecrypt`).
- `kms/` package: `KMS` interface (now with `CurrentVersion(ctx, keyID)
  (string, error)` for rotation bookkeeping) and `LocalDevKMS` dev adapter
  extracted from `services/spine/auth`. Cloud adapters (AWS KMS, GCP KMS,
  Vault Transit) implement the same interface.

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
