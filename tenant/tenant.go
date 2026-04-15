// Package tenant provides per-request tenant isolation for Omur services.
//
// Tenant ID is extracted from the X-Authentik-Uid header (set by Caddy's
// forward_auth after Authentik SSO) or the X-Tenant-ID header (set by
// internal service-to-service calls). The resolved ID is stored in the
// request context and retrieved with FromRequest / FromContext.
package tenant

import (
	"context"
	"net/http"
)

type ctxKey struct{}

// Middleware extracts the tenant ID from request headers and stores it in
// the request context. Resolution order:
//  1. X-Authentik-Uid (Caddy forward_auth — trusted, per-user)
//  2. X-Tenant-ID (internal service-to-service calls)
//
// If neither header is present the request proceeds without a tenant context;
// handlers must check for empty tenant and return 401.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := r.Header.Get("X-Authentik-Uid")
		if tid == "" {
			tid = r.Header.Get("X-Tenant-ID")
		}
		if tid != "" {
			ctx := context.WithValue(r.Context(), ctxKey{}, tid)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// FromContext returns the tenant ID from the context, or "".
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// FromRequest returns the tenant ID from the request context, or "".
func FromRequest(r *http.Request) string {
	return FromContext(r.Context())
}

// Require returns the tenant ID or writes a 401 error and returns "".
// Handlers should check: if tid := tenant.Require(w, r); tid == "" { return }
func Require(w http.ResponseWriter, r *http.Request) string {
	tid := FromRequest(r)
	if tid == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"tenant_id required — authenticate via Authentik or pass X-Tenant-ID header"}`))
	}
	return tid
}
