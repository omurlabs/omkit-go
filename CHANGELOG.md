# Changelog

## v0.2.0 — 2026-05-18

### Removed (BREAKING)

- **featureflags**: `ParseFromJSON` no longer accepts the legacy bare-bool
  shape. Rows of the form `true`/`false` in `app_settings.value_json` now
  return a zero (disabled) `Flag` instead of being upgraded to
  `{enabled: bool, roles: [admin, support, user]}`. Consumers that still
  store bare-bool rows must rewrap them into the object shape before
  upgrading.

## v0.1.2 — 2026-05-18

### Added

- **syncnotifier**: Test coverage for `NotifyMetrics` — request shape, headers, non-2xx handling, unreachable-server safety.

## v0.1.1 — 2026-05-18

### Changed

- **encryption**: Replace Fernet-compatible scheme with AES-256-GCM. Wire-compatible envelope shared with `omkit-python` (see `crypto` package and cross-SDK interop tests).

## v0.1.0 — 2026-05-17

Initial release.
