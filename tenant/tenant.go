// tenant.go — tenant module.
//
// exports: SourceAwareResolver | AuthDetails | AutoProvisionerV2 | MiddlewareConfig | Middleware | FromContext | FromRequest | NewContextForTest | Require
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package tenant provides per-request tenant isolation.
//
// Browser requests arrive with X-Auth-Request-User set by Caddy forward_auth
// (Zitadel via oauth2-proxy). The middleware maps the value to a tenant UUID
// via DB lookup with in-memory cache.
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

// SourceAwareResolver maps a (source, auth_uid) pair to a tenant UUID.
// Lookup predicate: WHERE source = $1 AND auth_uid = $2. Source is always
// "zitadel" post-cutover, but the column is preserved for forensic
// traceability of legacy rows.
type SourceAwareResolver func(ctx context.Context, source, authUID string) (tenantID string, err error)

// AuthDetails carries the fields needed to auto-provision a new user_auth_map
// row. Used by AutoProvisionerV2.
type AuthDetails struct {
	Source  string // IdP name: "zitadel"
	AuthUID string // bare value from X-Auth-Request-User (no namespace prefix)
	Email   string
}

// AutoProvisionerV2 creates a mapping for a new user in the given IdP.
type AutoProvisionerV2 func(ctx context.Context, d AuthDetails) (tenantID string, err error)

// MiddlewareConfig configures the tenant middleware.
type MiddlewareConfig struct {
	// ResolveV2 maps (source, auth_uid) → tenant_id using the user_auth_map.source
	// column.
	ResolveV2 SourceAwareResolver

	// AutoProvisionV2 is the source-aware auto-provisioner. Optional.
	AutoProvisionV2 AutoProvisionerV2

	// ServiceToken, if non-empty, gates both X-Tenant-ID (step 1) and the
	// browser-auth header path (step 3). Requests with X-Auth-Request-User
	// but no matching X-Service-Token are rejected.
	ServiceToken string

	// CacheTTL bounds (source, auth_uid) → tenant_id cache entry lifetime.
	// Zero means default (5 minutes).
	CacheTTL time.Duration

	// SessionResolver, if non-nil, is consulted for an omur_session cookie
	// before any header resolution. Nil is valid during the passkey-slice
	// rollout (spec cross-ref: 2026-04-22-sub-project-a-passkey-finalization).
	SessionResolver func(ctx context.Context, sessionID string) (tenantID string, err error)
}

// Middleware extracts the tenant ID from request headers and stores it in
// the request context. Resolution order:
//  1. X-Tenant-ID (internal service-to-service calls — direct UUID). When
//     ServiceToken is configured, also requires X-Service-Token match.
//  2. omur_session cookie (via SessionResolver), if configured.
//  3. X-Auth-Request-User (Zitadel via oauth2-proxy) → ResolveV2 → tenant UUID.
//
// If neither resolves, the request proceeds without tenant context;
// handlers must check via Require().
//
// The factory returns (func, error) so future startup validations can
// fail-closed without requiring callsite changes.
func Middleware(cfg MiddlewareConfig) (func(http.Handler) http.Handler, error) {
	cache := newUIDCache(cfg.CacheTTL)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tid := resolveTenant(r, &cfg, cache)
			if tid != "" {
				ctx := context.WithValue(r.Context(), ctxKey{}, tid)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func resolveTenant(r *http.Request, cfg *MiddlewareConfig, cache *uidCache) string {
	// Step 1: X-Tenant-ID + X-Service-Token (service-to-service).
	if tid := r.Header.Get("X-Tenant-ID"); tid != "" {
		if cfg.ServiceToken == "" || r.Header.Get("X-Service-Token") == cfg.ServiceToken {
			return tid
		}
		slog.Warn("tenant.xtenant_id_without_service_token",
			"tenant_id", tid, "remote_addr", r.RemoteAddr)
		return ""
	}

	// Step 2: omur_session cookie (passkey path).
	if cfg.SessionResolver != nil {
		if c, err := r.Cookie("omur_session"); err == nil && c.Value != "" {
			if tid, err := cfg.SessionResolver(r.Context(), c.Value); err == nil && tid != "" {
				return tid
			}
		}
	}

	// Step 3: browser-auth header path. Requires ServiceToken match when configured.
	const uidHeader = "X-Auth-Request-User"
	const emailHeader = "X-Auth-Request-Email"
	authUID := r.Header.Get(uidHeader)
	if authUID == "" {
		return ""
	}
	if cfg.ServiceToken != "" && r.Header.Get("X-Service-Token") != cfg.ServiceToken {
		slog.Warn("tenant.service_token_missing",
			"header", uidHeader, "remote_addr", r.RemoteAddr)
		return ""
	}

	const source = "zitadel"
	if cached := cache.get(source, authUID); cached != "" {
		return cached
	}

	var (
		tid string
		err error
	)
	if cfg.ResolveV2 != nil {
		tid, err = cfg.ResolveV2(r.Context(), source, authUID)
	}
	if err != nil {
		slog.Warn("tenant.resolve_failed", "source", source, "auth_uid", authUID, "error", err)
	}
	if tid == "" {
		email := r.Header.Get(emailHeader)
		if cfg.AutoProvisionV2 != nil {
			tid, err = cfg.AutoProvisionV2(r.Context(), AuthDetails{Source: source, AuthUID: authUID, Email: email})
		}
		if err != nil {
			slog.Warn("tenant.auto_provision_failed", "source", source, "auth_uid", authUID, "error", err)
		}
	}
	if tid != "" {
		cache.set(source, authUID, tid)
	}
	return tid
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
		w.Write([]byte(`{"error":"tenant_id required — authenticate via Zitadel or pass X-Tenant-ID"}`))
	}
	return tid
}

// uidCache is a TTL-bounded in-memory cache for (source, auth_uid) → tenant_id mappings.
// Entries expire so revoked IdP users stop authenticating automatically
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

func (c *uidCache) get(source, authUID string) string {
	key := source + ":" + authUID
	c.mu.RLock()
	entry, ok := c.m[key]
	c.mu.RUnlock()
	if !ok {
		return ""
	}
	if time.Now().After(entry.expires) {
		c.mu.Lock()
		if cur, ok := c.m[key]; ok && time.Now().After(cur.expires) {
			delete(c.m, key)
		}
		c.mu.Unlock()
		return ""
	}
	return entry.tenantID
}

func (c *uidCache) set(source, authUID, tenantID string) {
	key := source + ":" + authUID
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = uidCacheEntry{
		tenantID: tenantID,
		expires:  time.Now().Add(c.ttl),
	}
}
