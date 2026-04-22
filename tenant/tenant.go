// Package tenant provides per-request tenant isolation for Omur services.
//
// Browser requests arrive with an IdP-specific header set by Caddy
// forward_auth (X-Authentik-Uid for Authentik, X-Auth-Request-User for
// Zitadel). The middleware selects the right header via IDPMode and maps
// the value to a tenant UUID via DB lookup with in-memory cache.
//
// Internal service-to-service calls pass X-Tenant-ID directly (already a UUID).
package tenant

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type ctxKey struct{}

// Resolver maps a bare auth_uid to a tenant UUID for the Authentik-era
// single-source table. Kept for backward compatibility; new code should use
// SourceAwareResolver.
//
// Deprecated: use SourceAwareResolver + MiddlewareConfig.ResolveV2.
type Resolver func(ctx context.Context, authUID string) (tenantID string, err error)

// SourceAwareResolver maps a (source, auth_uid) pair to a tenant UUID.
// Lookup predicate: WHERE source = $1 AND auth_uid = $2. Source values:
// "authentik" | "zitadel".
type SourceAwareResolver func(ctx context.Context, source, authUID string) (tenantID string, err error)

// AuthDetails carries the fields needed to auto-provision a new user_auth_map
// row. Used by AutoProvisionerV2.
type AuthDetails struct {
	Source  string // IdP name: "authentik" | "zitadel"
	AuthUID string // bare value from the browser-auth header (no namespace prefix)
	Email   string
}

// AutoProvisioner creates a mapping for a new Authentik user (single-source).
// Deprecated: use AutoProvisionerV2 + MiddlewareConfig.AutoProvisionV2.
type AutoProvisioner func(ctx context.Context, authUID, email string) (tenantID string, err error)

// AutoProvisionerV2 creates a mapping for a new user in the given IdP.
type AutoProvisionerV2 func(ctx context.Context, d AuthDetails) (tenantID string, err error)

// MiddlewareConfig configures the tenant middleware.
type MiddlewareConfig struct {
	// Resolve is the legacy single-source resolver. Deprecated — set ResolveV2 instead.
	Resolve Resolver

	// ResolveV2 maps (source, auth_uid) → tenant_id using the user_auth_map.source
	// column added in 2026-04-22. When both Resolve and ResolveV2 are set, V2 wins.
	ResolveV2 SourceAwareResolver

	// AutoProvision is the legacy auto-provisioner. Deprecated — set AutoProvisionV2.
	AutoProvision AutoProvisioner

	// AutoProvisionV2 is the source-aware auto-provisioner. When both are set, V2 wins.
	AutoProvisionV2 AutoProvisionerV2

	// ServiceToken, if non-empty, gates both X-Tenant-ID (step 1) and the
	// browser-auth header path (step 3). Requests with X-Auth-Request-User or
	// X-Authentik-Uid but no matching X-Service-Token are rejected.
	ServiceToken string

	// CacheTTL bounds (source, auth_uid) → tenant_id cache entry lifetime.
	// Zero means default (5 minutes).
	CacheTTL time.Duration

	// IDPMode selects the browser-auth header family. Valid: IDPModeAuthentik
	// | IDPModeZitadel. Empty defaults to IDPModeAuthentik with a slog.Warn.
	IDPMode string

	// SessionResolver, if non-nil, is consulted for an omur_session cookie
	// before any header resolution. Nil is valid during the passkey-slice
	// rollout (spec cross-ref: 2026-04-22-sub-project-a-passkey-finalization).
	SessionResolver func(ctx context.Context, sessionID string) (tenantID string, err error)

	// RetireAuthentik, when true, rejects any configuration that still
	// references the Authentik header family. Set from OMUR_IDP_RETIRE_AUTHENTIK.
	RetireAuthentik bool

	// PrivacyRetireAuthentik reflects the existing OMUR_PRIVACY_LAYER_RETIRE_AUTHENTIK
	// flag. Only used by the startup assertion (see validateConfig).
	PrivacyRetireAuthentik bool
}

// Middleware extracts the tenant ID from request headers and stores it in
// the request context. Resolution order:
//  1. X-Tenant-ID (internal service-to-service calls — direct UUID). When
//     ServiceToken is configured, also requires X-Service-Token match.
//  2. omur_session cookie (via SessionResolver), if configured.
//  3. Browser-auth UID header (IDPMode-selected) → Resolver → tenant UUID.
//
// If neither resolves, the request proceeds without tenant context;
// handlers must check via Require().
func Middleware(cfg MiddlewareConfig) (func(http.Handler) http.Handler, error) {
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

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

func validateConfig(cfg *MiddlewareConfig) error {
	if cfg.IDPMode == "" {
		slog.Warn("tenant.middleware.idp_mode_empty_defaulting_to_authentik")
		cfg.IDPMode = IDPModeAuthentik
	}
	if cfg.IDPMode != IDPModeAuthentik && cfg.IDPMode != IDPModeZitadel {
		return fmt.Errorf("tenant: unknown IDPMode %q (want authentik or zitadel)", cfg.IDPMode)
	}
	if cfg.IDPMode == IDPModeAuthentik && cfg.RetireAuthentik {
		return fmt.Errorf("tenant: OMUR_IDP=authentik with OMUR_IDP_RETIRE_AUTHENTIK=true is contradictory")
	}
	if cfg.RetireAuthentik && !cfg.PrivacyRetireAuthentik {
		return fmt.Errorf("tenant: OMUR_IDP_RETIRE_AUTHENTIK requires OMUR_PRIVACY_LAYER_RETIRE_AUTHENTIK=true")
	}
	return nil
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
	uidHeader := BrowserUIDHeader(cfg.IDPMode)
	authUID := r.Header.Get(uidHeader)
	if authUID == "" {
		return ""
	}
	if cfg.ServiceToken != "" && r.Header.Get("X-Service-Token") != cfg.ServiceToken {
		slog.Warn("tenant.service_token_missing",
			"header", uidHeader, "remote_addr", r.RemoteAddr)
		return ""
	}

	source := cfg.IDPMode
	if cached := cache.get(source, authUID); cached != "" {
		return cached
	}

	// Resolver V2 preferred; fall back to V1 for backward compat.
	var (
		tid string
		err error
	)
	if cfg.ResolveV2 != nil {
		tid, err = cfg.ResolveV2(r.Context(), source, authUID)
	} else if cfg.Resolve != nil {
		tid, err = cfg.Resolve(r.Context(), authUID)
	}
	if err != nil {
		slog.Warn("tenant.resolve_failed", "source", source, "auth_uid", authUID, "error", err)
	}
	if tid == "" {
		email := r.Header.Get(BrowserEmailHeader(cfg.IDPMode))
		if cfg.AutoProvisionV2 != nil {
			tid, err = cfg.AutoProvisionV2(r.Context(), AuthDetails{Source: source, AuthUID: authUID, Email: email})
		} else if cfg.AutoProvision != nil {
			tid, err = cfg.AutoProvision(r.Context(), authUID, email)
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
		w.Write([]byte(`{"error":"tenant_id required — authenticate via the configured IdP or pass X-Tenant-ID"}`))
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
