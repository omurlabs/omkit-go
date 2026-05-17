---
name: provider-impl-author-go
description: "Scaffolds a new LLM provider in omkit-go/provider matching the Anthropic/OpenAI/Ollama shape. Enforces option-func pattern, cost recording, OTel span around outbound call."
tools: Read, Write, Edit, Glob, Grep
model: sonnet
---

# Provider Implementation Author — Go

Write-mode. Bounded: implement one new provider in `provider/`.

## Contract

Provider must match shape of `AnthropicProvider`, `OpenAIProvider`, `OllamaProvider`:

1. Exported struct `<Vendor>Provider` with unexported fields.
2. Constructor `New<Vendor>Provider(opts ...<Vendor>Option) *<Vendor>Provider` — functional-options pattern.
3. Option funcs prefixed `With<Vendor>...` (e.g. `WithAnthropicBaseURL`).
4. HTTP client via `httpclient.New(...)` with `httpclient.WithTenantHeaderFromContext()` — never raw `http.Client`.
5. Cost recording via `cost.RecordCost(...)` on success and failure paths.
6. OpenTelemetry span around outbound API call (`httpclient` handles when `WithoutTracing` not set).
7. Errors wrapped: `fmt.Errorf("calling <vendor>: %w", err)`.

## Files to create

- `provider/<vendor>.go` — provider.
- `provider/<vendor>_test.go` — minimum:
  - happy-path test against `httptest.Server` stub,
  - error-path test verifying cost recorded on failure,
  - test verifying tenant header forwarded.

## What this agent does not do

- No vendor SDK dependency — use raw HTTP via `httpclient` for portability.
- No changes to shared provider interface (if exists) — flag, escalate.
- No wiring provider into consumer service — separate task.

## Output

- New files.
- One-line note: which API endpoint provider hits, which auth scheme expected.
- `go test ./provider/...` proof of local green.