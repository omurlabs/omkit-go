package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stubResolver returns a fixed tenant ID for any (source, auth UID).
func stubResolver(tenantID string) SourceAwareResolver {
	return func(_ context.Context, _ string, _ string) (string, error) {
		return tenantID, nil
	}
}

// noopHandler is a handler that writes 200 and the resolved tenant ID.
func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := FromRequest(r)
		if tid == "" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("no-tenant"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tid))
	})
}

// mustMW builds the middleware and fails the test if the factory errors.
func mustMW(t *testing.T, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	t.Helper()
	mw, err := Middleware(cfg)
	if err != nil {
		t.Fatalf("tenant.Middleware: %v", err)
	}
	return mw
}

func TestMiddleware_XTenantID_Direct(t *testing.T) {
	mw := mustMW(t, MiddlewareConfig{})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("X-Tenant-ID: got %q, want UUID", got)
	}
}

func TestMiddleware_ZitadelUID_Resolved(t *testing.T) {
	mw := mustMW(t, MiddlewareConfig{
		ResolveV2: stubResolver("11111111-2222-3333-4444-555555555555"),
	})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Auth-Request-User", "zitadel-sub-123")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("Zitadel UID: got %q, want resolved tenant", got)
	}
}

func TestMiddleware_ResolverGetsZitadelSource(t *testing.T) {
	var gotSource, gotAuthUID string
	cfg := MiddlewareConfig{
		ResolveV2: func(_ context.Context, source, authUID string) (string, error) {
			gotSource, gotAuthUID = source, authUID
			return "tenant-id", nil
		},
	}
	mw := mustMW(t, cfg)(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Auth-Request-User", "198261369861120001")
	mw.ServeHTTP(httptest.NewRecorder(), req)

	if gotSource != "zitadel" {
		t.Errorf("source = %q, want zitadel", gotSource)
	}
	if gotAuthUID != "198261369861120001" {
		t.Errorf("authUID = %q, want 198261369861120001", gotAuthUID)
	}
}

func TestMiddleware_XTenantID_TakesPrecedence_OverZitadel(t *testing.T) {
	// If both X-Tenant-ID and X-Auth-Request-User are set, X-Tenant-ID wins.
	mw := mustMW(t, MiddlewareConfig{
		ResolveV2: stubResolver("should-not-see-this"),
	})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "direct-tenant-id")
	req.Header.Set("X-Auth-Request-User", "zitadel-sub-123")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "direct-tenant-id" {
		t.Errorf("priority: got %q, want direct-tenant-id", got)
	}
}

func TestMiddleware_NoHeaders_NoTenant(t *testing.T) {
	mw := mustMW(t, MiddlewareConfig{})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("no headers: got %q, want no-tenant", got)
	}
}

func TestMiddleware_FakeZitadelUID_WithoutResolver_NoTenant(t *testing.T) {
	// Attacker sends forged X-Auth-Request-User but no resolver is set.
	// Middleware must NOT trust the header value as a tenant ID directly.
	mw := mustMW(t, MiddlewareConfig{})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Auth-Request-User", "attacker-forged-uid")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("forged UID without resolver: got %q, want no-tenant", got)
	}
}

func TestMiddleware_ZitadelUID_UnknownUser_NoAutoProvision(t *testing.T) {
	// Resolver returns empty (unknown user), no auto-provisioner → no tenant.
	mw := mustMW(t, MiddlewareConfig{
		ResolveV2: stubResolver(""),
	})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Auth-Request-User", "unknown-user-sub")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("unknown user: got %q, want no-tenant", got)
	}
}

func TestMiddleware_XTenantID_RequiresServiceToken_WhenConfigured(t *testing.T) {
	// ServiceToken is configured — X-Tenant-ID alone is not trusted.
	mw := mustMW(t, MiddlewareConfig{ServiceToken: "secret"})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	// No X-Service-Token → tenant must be dropped.
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("X-Tenant-ID without service token: got %q, want no-tenant", got)
	}
}

func TestMiddleware_XTenantID_WrongServiceToken_Rejected(t *testing.T) {
	mw := mustMW(t, MiddlewareConfig{ServiceToken: "secret"})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	req.Header.Set("X-Service-Token", "wrong")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("X-Tenant-ID with wrong service token: got %q, want no-tenant", got)
	}
}

func TestMiddleware_XTenantID_CorrectServiceToken_Accepted(t *testing.T) {
	mw := mustMW(t, MiddlewareConfig{ServiceToken: "secret"})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	req.Header.Set("X-Service-Token", "secret")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("X-Tenant-ID with service token: got %q, want tenant UUID", got)
	}
}

func TestUIDCache_Expires(t *testing.T) {
	c := newUIDCache(50 * time.Millisecond)
	c.set("zitadel", "uid1", "tenant1")
	if got := c.get("zitadel", "uid1"); got != "tenant1" {
		t.Fatalf("fresh entry: got %q, want %q", got, "tenant1")
	}
	time.Sleep(75 * time.Millisecond)
	if got := c.get("zitadel", "uid1"); got != "" {
		t.Fatalf("expired entry: got %q, want empty", got)
	}
}

func TestUIDCache_Refreshes(t *testing.T) {
	c := newUIDCache(100 * time.Millisecond)
	c.set("zitadel", "uid1", "tenant1")
	time.Sleep(60 * time.Millisecond)
	c.set("zitadel", "uid1", "tenant1") // re-set resets TTL
	time.Sleep(60 * time.Millisecond)
	if got := c.get("zitadel", "uid1"); got != "tenant1" {
		t.Fatalf("refreshed entry: got %q, want %q", got, "tenant1")
	}
}

func TestUIDCache_SourceIsolation(t *testing.T) {
	// Same auth_uid under different sources must not collide in cache.
	// Source namespacing is preserved on the cache key even though only
	// one source is live post-cutover — keeps the key shape stable.
	c := newUIDCache(time.Minute)
	c.set("zitadel", "collision", "tenant-zitadel")
	c.set("other", "collision", "tenant-other")
	if got := c.get("zitadel", "collision"); got != "tenant-zitadel" {
		t.Errorf("zitadel cache: got %q", got)
	}
	if got := c.get("other", "collision"); got != "tenant-other" {
		t.Errorf("other cache: got %q", got)
	}
}

func TestMiddleware_CacheRevocation(t *testing.T) {
	calls := 0
	resolver := func(_ context.Context, _ string, uid string) (string, error) {
		calls++
		return "tenant-" + uid, nil
	}
	mw := mustMW(t, MiddlewareConfig{
		ResolveV2: resolver,
		CacheTTL:  50 * time.Millisecond,
	})(noopHandler())

	req := func() *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Auth-Request-User", "alice")
		return r
	}

	w1 := httptest.NewRecorder()
	mw.ServeHTTP(w1, req())
	if calls != 1 {
		t.Fatalf("first call: resolver hit %d times, want 1", calls)
	}

	w2 := httptest.NewRecorder()
	mw.ServeHTTP(w2, req())
	if calls != 1 {
		t.Fatalf("cached call: resolver hit %d times, want still 1", calls)
	}

	time.Sleep(75 * time.Millisecond)
	w3 := httptest.NewRecorder()
	mw.ServeHTTP(w3, req())
	if calls != 2 {
		t.Fatalf("post-expiry call: resolver hit %d times, want 2", calls)
	}
}

func TestRequire_Returns401_WhenNoTenant(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tid := Require(w, r); tid == "" {
			return
		}
		w.Write([]byte("should-not-reach"))
	})

	mw := mustMW(t, MiddlewareConfig{})(handler)
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Require without tenant: got %d, want 401", w.Code)
	}
}
