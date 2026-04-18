// Package tenant provides per-request tenant isolation for Omur services.
//
// Browser requests arrive with X-Authentik-Uid (set by Caddy forward_auth
// after Authentik SSO). This is Authentik's internal user pk — NOT a UUID.
// A Resolver maps it to the tenant UUID via DB lookup with in-memory cache.
//
// Internal service-to-service calls pass X-Tenant-ID directly (already a UUID).
package tenant

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
)

type ctxKey struct{}

// Resolver maps an Authentik user pk to a tenant UUID.
// Returns ("", nil) if no mapping exists — the middleware will auto-provision.
type Resolver func(ctx context.Context, authUID string) (tenantID string, err error)

// AutoProvisioner creates a mapping for a new Authentik user.
// Called once per unknown auth_uid, with the email from X-Authentik-Email.
type AutoProvisioner func(ctx context.Context, authUID, email string) (tenantID string, err error)

// MiddlewareConfig configures the tenant middleware.
type MiddlewareConfig struct {
	Resolve       Resolver
	AutoProvision AutoProvisioner // optional: auto-create mapping for new users
	// ServiceToken, if non-empty, gates the X-Tenant-ID header: the request must
	// also carry X-Service-Token: <ServiceToken> for X-Tenant-ID to be trusted.
	// This prevents a compromised peer on the backend network from impersonating
	// any tenant by forging a UUID header. Leave empty in dev/tests.
	ServiceToken string
}

// Middleware extracts the tenant ID from request headers and stores it in
// the request context. Resolution order:
//  1. X-Tenant-ID (internal service-to-service calls — direct UUID). When
//     ServiceToken is configured, also requires X-Service-Token match.
//  2. X-Authentik-Uid → Resolver → tenant UUID (browser requests via Caddy)
//
// If neither resolves, the request proceeds without tenant context;
// handlers must check via Require().
func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	cache := &uidCache{m: map[string]string{}}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Priority 1: explicit X-Tenant-ID (internal calls, smoke tests).
			// Require matching X-Service-Token when ServiceToken is configured.
			tid := r.Header.Get("X-Tenant-ID")
			if tid != "" && cfg.ServiceToken != "" {
				if r.Header.Get("X-Service-Token") != cfg.ServiceToken {
					slog.Warn("tenant.xtenant_id_without_service_token",
						"tenant_id", tid, "remote_addr", r.RemoteAddr)
					tid = ""
				}
			}

			// Priority 2: Authentik UID → cached DB lookup
			if tid == "" {
				if authUID := r.Header.Get("X-Authentik-Uid"); authUID != "" {
					tid = cache.get(authUID)
					if tid == "" && cfg.Resolve != nil {
						resolved, err := cfg.Resolve(r.Context(), authUID)
						if err != nil {
							slog.Warn("tenant.resolve_failed", "auth_uid", authUID, "error", err)
						} else if resolved != "" {
							tid = resolved
							cache.set(authUID, tid)
						}
					}
					// Auto-provision if still no mapping
					if tid == "" && cfg.AutoProvision != nil {
						email := r.Header.Get("X-Authentik-Email")
						provisioned, err := cfg.AutoProvision(r.Context(), authUID, email)
						if err != nil {
							slog.Warn("tenant.auto_provision_failed", "auth_uid", authUID, "error", err)
						} else if provisioned != "" {
							tid = provisioned
							cache.set(authUID, tid)
						}
					}
				}
			}

			if tid != "" {
				ctx := context.WithValue(r.Context(), ctxKey{}, tid)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
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

// uidCache is a simple in-memory cache for auth_uid → tenant_id mappings.
type uidCache struct {
	mu sync.RWMutex
	m  map[string]string
}

func (c *uidCache) get(authUID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.m[authUID]
}

func (c *uidCache) set(authUID, tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[authUID] = tenantID
}
