// requestid.go — requestid module.
//
// exports: HeaderName | NewContext | FromContext | WithRequestIDPropagation
// rules:   none
// agent:   batch-d-overnight | claude | 2026-05-02 | claude | initial implementation
// message: SDK helper for X-Request-ID propagation across service hops

// Package requestid centralises the X-Request-ID correlation identifier:
//   - context key (so handlers and outbound clients share one source of truth)
//   - HTTP header constant (so callers don't string-literal it everywhere)
//   - WithRequestIDPropagation helper that copies the id from ctx onto an
//     outbound *http.Request before it is dispatched.
//
// Spine ingress (services/spine/middleware/correlation_id.go) installs the id
// into ctx after validating it as UUID-v4. Downstream callers retrieve it via
// FromContext or by using the SDK httpclient option WithRequestIDFromContext
// (defined in the httpclient package).
package requestid

import (
	"context"
	"net/http"
)

// HeaderName is the canonical wire header for the per-request correlation id.
// All Omur services emit and accept this header on every hop.
const HeaderName = "X-Request-ID"

type ctxKey struct{}

// NewContext returns a derived context with the given request id installed.
// Empty id is a no-op (returns ctx unchanged) — keeps middleware call sites
// branch-free.
func NewContext(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the request id stored in ctx, or "" if absent.
// Callers should prefer this over re-parsing the inbound header so the id
// keeps flowing across goroutine and timeout boundaries.
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// WithRequestIDPropagation copies the request id from ctx onto req's
// X-Request-ID header. No-op when ctx has no id or req is nil. Existing
// header values on req are preserved (caller wins) — this matches how
// SDK auth helpers behave.
//
// Use this for ad-hoc *http.Request callsites that do not go through
// httpclient.Client. For Client-based callsites prefer the dedicated option
// (see httpclient.WithRequestIDFromContext).
func WithRequestIDPropagation(ctx context.Context, req *http.Request) {
	if req == nil {
		return
	}
	if req.Header.Get(HeaderName) != "" {
		return
	}
	if id := FromContext(ctx); id != "" {
		if req.Header == nil {
			req.Header = http.Header{}
		}
		req.Header.Set(HeaderName, id)
	}
}
