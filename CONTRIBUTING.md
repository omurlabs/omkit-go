# Contributing to omkit-go

> Status: pre-1.0 — the public Go API may break across minor versions.
> Breaking changes are marked in [`CHANGELOG.md`](CHANGELOG.md). The
> v0.x → v0.y bump is the semver-major signal on the v0 line.

omkit-go is the Go-side SDK in the omurlabs SaaS-scaffolding pair
([`omkit-python`](https://github.com/omurlabs/omkit-python) is the
Python counterpart). The two share a wire-compatible AES-256-GCM
envelope and a shared job-queue envelope.

## Contribution stance

**Open with guardrails.** We welcome:

- Bug fixes against documented behaviour.
- Documentation fixes and additions.
- Tests (unit and integration).
- New packages that fit the SaaS-scaffolding shape (multi-tenant
  isolation, encryption-at-rest, observability, async eventing).

For these we accept PRs without prior discussion.

We require **maintainer pre-approval before code lands** for:

- Breaking changes to any exported package — even on the v0 line.
  These bump the minor version.
- Changes to the AES-256-GCM envelope or to job-queue envelope
  serialisation. Both must stay wire-compatible with `omkit-python`.
- New cross-SDK contracts (anything that the Python side also has to
  understand on the wire).
- Anything that touches `crypto/`, `encryption/`, `kms/`, or the
  tenant-isolation primitives in `tenant/` / `dbpool/`.

For these, please open an issue first (or a draft PR) so we can
align on the design before code review.

## Getting started

### Prerequisites

- Go 1.26+ (the toolchain version pinned in `go.mod`).
- Docker (or Podman) for the integration test compose stack —
  Postgres 16 + Valkey 8.

### Local setup

```bash
git clone https://github.com/omurlabs/omkit-go.git
cd omkit-go
go mod download
go build ./...
go test ./...
```

Integration tests that touch Postgres / Valkey expect the standard
envs (`TEST_POSTGRES_DSN`, `TEST_REDIS_ADDR`). The
[`scripts/test-with-postgres.sh`](scripts/test-with-postgres.sh)
helper brings up an ephemeral docker-compose stack and runs the suite
against it.

## Coding standards

| Concern | Tool | Scope |
|---|---|---|
| Format | `gofmt` / `goimports` | full tree |
| Vet | `go vet ./...` | full tree |
| Tests | `go test -race -count=1 ./...` | full tree |
| Vuln scan | `govulncheck ./...` | full tree |
| Secrets | `gitleaks` | full history |

CI runs all of the above. Race detector is on for every PR.

### Style notes

- Public packages document *why*, not *what*. Identifiers should
  already describe what.
- Every package that touches tenant data takes a `context.Context`
  carrying the tenant scope; there is no implicit fallback.
- Errors wrap with `fmt.Errorf("...: %w", err)` so callers can
  `errors.Is` / `errors.As` against sentinels.
- `slog` is the structured-logging surface. No package logs to a
  global logger; consumers inject a `*slog.Logger` or rely on the
  default.

## Workflow

### Branch + PR shape

- Branch per feature; one feature per PR.
- PR title: `<area>: <imperative description>` (e.g.
  `featureflags: drop legacy bare-bool fallback`).
- Every PR includes test changes, or an explicit `no-test-needed:`
  rationale in the PR body.
- Breaking changes use `!` after the type/scope and bump the minor
  version (e.g. `feat(featureflags)!: drop legacy bare-bool
  fallback`).

### Commit messages

[Conventional Commits](https://www.conventionalcommits.org/):

- `feat(area): …` — new behaviour
- `fix(area): …` — bug fix
- `docs: …`, `chore: …`, `ci: …`, `test: …`, `refactor: …`,
  `perf: …`, `build: …`, `revert: …`

Body explains *why*. The diff shows *what*. Reference issues when
relevant.

### Tests

```bash
go test -race -count=1 ./...
go test -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -func=coverage.out
```

Unit tests live next to the package they cover (`pkg_test.go`).
Integration tests that need Postgres / Valkey are guarded by the
`TEST_POSTGRES_DSN` / `TEST_REDIS_ADDR` envs and skip cleanly when
unset.

## Cross-SDK envelope

When changing `crypto/`, `encryption/`, or any `jobqueue/` envelope:

1. Mirror the change in
   [`omkit-python`](https://github.com/omurlabs/omkit-python) in the
   same release cycle.
2. Update the cross-SDK interop tests on both sides.
3. Call out the wire-compat impact in the PR body. We do not ship
   envelope-incompatible releases.

## Reporting bugs and asking questions

- **Bug reports**: open an issue against this repo.
- **Security vulnerabilities**: do not open a public issue. See
  [`SECURITY.md`](SECURITY.md) for the private disclosure path.

## License

By contributing you agree your contributions are licensed under
[Apache-2.0](LICENSE) (inbound = outbound). No CLA is required.
