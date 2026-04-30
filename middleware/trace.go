// trace.go — trace module.
//
// exports: Trace
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Trace returns middleware wrapping the handler with OpenTelemetry server-side
// instrumentation. Each request gets a span with method/route/status attributes.
package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Trace wraps an http.Handler with otelhttp.NewHandler, creating a server span
// per request. Use in conjunction with tracing.Init so spans are exported.
func Trace(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, serviceName)
	}
}
