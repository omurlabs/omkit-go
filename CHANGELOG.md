# Changelog

## v0.1.2 — 2026-05-18

### Added

- **syncnotifier**: Test coverage for `NotifyMetrics` — request shape, headers, non-2xx handling, unreachable-server safety.

## v0.1.1 — 2026-05-18

### Changed

- **encryption**: Replace Fernet-compatible scheme with AES-256-GCM. Wire-compatible envelope shared with `omkit-python` (see `crypto` package and cross-SDK interop tests).

## v0.1.0 — 2026-05-17

Initial release.
