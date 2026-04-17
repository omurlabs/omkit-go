# omur-go-sdk

Shared infrastructure for Omur Go services — config loading, logging, tracing,
metrics, HTTP middleware, an HTTP client with OTel propagation, and provider
abstractions.

## Init order

Every Go service `main()` must call SDK helpers in this order:

```go
func main() {
    // 1. Logging (so subsequent calls log structured)
    logger := logging.Init("my-service")

    // 2. Config (depends on env vars only — no I/O)
    cfg := loadConfig() // wraps sdkconfig.Load() into a service-specific Config

    // 3. Tracing (uses ctx, returns shutdown func)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    shutdown, err := tracing.Init(ctx, "my-service", cfg.AppVersion)
    if err != nil {
        slog.Warn("tracing init failed (non-fatal)", "err", err)
    } else {
        defer func() {
            sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer scancel()
            _ = shutdown(sctx)
        }()
    }

    // 4. Server wiring (handlers, mux, middleware, http.Server)
    // ...
}
```

**Why this order:**
- Logging first so config-load errors surface as structured logs.
- Config has no dependencies, can come anywhere before server wiring.
- Tracing needs `ctx` and runs after config because it reads `OTLP_ENDPOINT` etc.
- The shutdown defer uses a fresh `context.Background()` with timeout, **not** the cancelled `ctx` — otherwise BatchSpanProcessor may no-op and drop in-flight spans.

## Middleware chain (canonical)

Inside-out wrapping order:

```
mux → metrics → tenantMw → Auth → CORS → RequestLog → Trace (outermost)
```

```go
var h http.Handler = mux
h = metrics.HTTPMiddlewareWithExclusions("my-service", metrics.DefaultMetricsExclusions...)(h)
// Tenant middleware (optional): resolves X-Tenant-ID / auto-provisions tenant
// from the token and binds tenant contextvars. Goes BETWEEN metrics and
// BearerAuth so metrics see the resolved tenant but auth still gates.
// Omit if the service doesn't resolve tenants from the request (e.g., reflex).
tenantMw := tenant.Middleware(tenant.MiddlewareConfig{
    Resolve:       newTenantResolver(pool),
    AutoProvision: newTenantAutoProvisioner(pool),
})
h = tenantMw(h)
h = middleware.BearerAuth(cfg.TenantToken, h) // optional
h = middleware.CORS(cfg.CORSOriginsList(), h)
h = middleware.RequestLog(h)
h = middleware.Trace("my-service")(h)
```

**Rationale:**
- **Trace outermost**: every request gets a span, including auth-rejected ones — useful for debugging "why is auth blocking everything".
- **RequestLog inside Trace**: log lines carry the trace ID since the span is open.
- **Auth inside CORS**: CORS pre-flight (OPTIONS) bypasses auth.
- **Metrics innermost**: counts only handler-reaching requests. Auth-rejected requests show up in logs/traces but not in `http_requests_total`. To break out 401s, use the existing `code` label on `http_requests_total`.
- **tenantMw between metrics and Auth**: metrics count already-labelled tenant requests; auth still decides the request. Omit if the service has no tenant resolution.

Use `metrics.HTTPMiddlewareWithExclusions("svc", metrics.DefaultMetricsExclusions...)` (not the bare `HTTPMiddleware`) so `/metrics`, `/health`, `/healthz`, `/ready` don't pollute counters.

## Config: embed `config.Base`

Most services need Postgres + Valkey + Ollama defaults. Embed the SDK base type:

```go
import sdkconfig "github.com/omurlabs/omur-core/packages/omur-go-sdk/config"

type Config struct {
    sdkconfig.Base
    // service-specific fields
    Port         int
    QueueBackend string
}

func Load() *Config {
    return &Config{
        Base:         sdkconfig.Load(),
        Port:         sdkconfig.EnvInt("MY_PORT", 8000),
        QueueBackend: sdkconfig.EnvStr("MY_QUEUE", "valkey"),
    }
}
```

For services that don't need Postgres/Valkey/Ollama (e.g. reflex), don't embed
`Base` — declare the fields you actually use, and call `sdkconfig.EnvStr` /
`sdkconfig.EnvInt` / `sdkconfig.EnvBool` / `sdkconfig.EnvInt64` directly.

## HTTP client

Use `httpclient.New(...)` for all outbound HTTP. It configures sensible
timeouts and wraps the transport in `otelhttp` for trace propagation.

```go
client := httpclient.New(
    httpclient.WithTimeout(10 * time.Second),
    httpclient.WithServiceToken(cfg.TenantToken), // optional
)
resp, err := client.Do(ctx, req)
```

**`WithServiceToken` rules:**
- Use it for clients that **only** call internal Omur services (token sent on every request).
- For clients shared between internal + external calls (e.g. reflex `longHTTP` → Frontal/Auris **and** api.telegram.org/graph.facebook.com), do **not** apply `WithServiceToken` — set `X-Service-Token` manually per-call so the token never leaks to third-party APIs.

**Hoist clients to package-level vars.** Constructing a new client per call
recreates the transport each time, loses any circuit-breaker state the
client accumulates, and breaks trace propagation if the request ctx isn't
forwarded.

### Options

| Option | Default | Notes |
|--------|---------|-------|
| `WithTimeout(d)` | 30s | Request timeout applied to `client.Do`. |
| `WithServiceToken(tok)` | none | Sets `X-Service-Token` on every outbound request. Only use for clients that talk exclusively to internal Omur services (see rules above). |
| `WithTransport(rt)` | `http.DefaultTransport` with tuned dial/idle settings | Use when a caller needs custom transport tuning (e.g. solid-sync CSS client). When tracing is enabled, the SDK wraps the provided transport in `otelhttp.NewTransport` so span propagation still works. |

## Packages

| Package | Purpose |
|---------|---------|
| `config` | `Base` env-var loader + `EnvStr`/`EnvInt`/`EnvInt64`/`EnvBool` helpers |
| `logging` | `Init(serviceName)` returns a structured slog.Logger |
| `tracing` | `Init(ctx, serviceName, version)` sets up OTel + returns shutdown |
| `metrics` | Prometheus helpers + `Handler()` + `HTTPMiddlewareWithExclusions` + `DefaultMetricsExclusions` |
| `middleware` | `Trace`, `CORS`, `RequestLog`, `BearerAuth` |
| `httpclient` | Outbound HTTP with OTel propagation (optional circuit breaker + service token) |
| `tenant` | Tenant resolution + auto-provisioning middleware |
| `provider` | Anthropic / OpenAI / Ollama LLM provider adapters |
| `health`, `dbpool`, `valkeystream`, `valkeysub`, `encryption`, `judge`, `registry`, `settings`, `syncnotifier`, `cerebellum` | service-specific helpers |
