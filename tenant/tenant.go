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
	"time"
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
	// CacheTTL bounds how long auth_uid → tenant_id entries live in the
	// in-memory cache. Zero means default (5 minutes).
	CacheTTL time.Duration
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
	cache := newUIDCache(cfg.CacheTTL)

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

// NewContextForTest returns ctx with the given tenant ID installed in the
// SDK's private context key. Intended for tests that need to exercise
// downstream helpers (e.g. httpclient.WithTenantHeaderFromContext) without
// running the full middleware stack.
func NewContextForTest(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, tenantID)
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

// uidCache is a TTL-bounded in-memory cache for auth_uid → tenant_id mappings.
// Entries expire so revoked Authentik users stop authenticating automatically
// once their mapping ages past the TTL (default 5 minutes via MiddlewareConfig).
type uidCache struct {
	mu  sync.RWMutex
	m   map[string]uidCacheEntry
	ttl time.Duration
}

type uidCacheEntry struct {
	tenantID string
	expires  time.Time
}

func newUIDCache(ttl time.Duration) *uidCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &uidCache{m: map[string]uidCacheEntry{}, ttl: ttl}
}

func (c *uidCache) get(authUID string) string {
	c.mu.RLock()
	entry, ok := c.m[authUID]
	c.mu.RUnlock()
	if !ok {
		return ""
	}
	if time.Now().After(entry.expires) {
		c.mu.Lock()
		// Double-check under write lock before deleting so a concurrent
		// refresh isn't lost.
		if cur, ok := c.m[authUID]; ok && time.Now().After(cur.expires) {
			delete(c.m, authUID)
		}
		c.mu.Unlock()
		return ""
	}
	return entry.tenantID
}

func (c *uidCache) set(authUID, tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[authUID] = uidCacheEntry{
		tenantID: tenantID,
		expires:  time.Now().Add(c.ttl),
	}
}
