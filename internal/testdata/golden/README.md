# Cross-runtime golden fixtures

Wire-compatibility between `omkit-go` and [`omkit-python`](https://github.com/omurlabs/omkit-python) is the headline differentiator of omkit. This directory pins it as fixtures instead of prose.

## Files

| File | Pinned contract |
|------|------------------|
| `envelope.json` | `jobqueue.Wrap` / `Unwrap` produce and accept byte-identical envelopes versus `omkit.jobqueue.wrap` / `unwrap`. |
| `encryption.json` | `encryption.Decrypt` reads tokens produced by either SDK. |
| `settings.json`   | Tenant settings ciphertext (anthropic / openai / openrouter API key shapes) stays readable after refactors. |

## Regenerating

Tokens carry random nonces; envelopes are deterministic. Regen via:

```bash
go run scripts/regen-golden.go
```

The script is the **only** sanctioned producer of these files. Do not hand-edit
JSON — the format is asserted by tests on both sides of the SDK boundary.

## Cross-SDK constraint: payload key ordering

Go's `encoding/json` marshals map keys in alphabetical order; Python preserves
dict insertion order. For envelope payloads to byte-match on both sides, each
fixture payload **must** use either:

1. A single top-level key, or
2. Multiple keys inserted in alphabetical order on the Python generator side.

Tests on both repos read this same JSON; deviation surfaces immediately.

## Bumping `EnvelopeVersion`

`jobqueue/envelope_golden_test.go` asserts `EnvelopeVersion == 1`. Bumping the
constant without regenerating fixtures (and updating the assertion + Python
side) fails CI by design.
