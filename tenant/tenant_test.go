package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubResolver returns a fixed tenant ID for any auth UID.
func stubResolver(tenantID string) Resolver {
	return func(_ context.Context, _ string) (string, error) {
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

func TestMiddleware_XTenantID_Direct(t *testing.T) {
	mw := Middleware(MiddlewareConfig{})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("X-Tenant-ID: got %q, want UUID", got)
	}
}

func TestMiddleware_AuthentikUID_Resolved(t *testing.T) {
	mw := Middleware(MiddlewareConfig{
		Resolve: stubResolver("11111111-2222-3333-4444-555555555555"),
	})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Authentik-Uid", "auth-pk-123")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("Authentik UID: got %q, want resolved tenant", got)
	}
}

func TestMiddleware_XTenantID_TakesPrecedence_OverAuthentik(t *testing.T) {
	// If both X-Tenant-ID and X-Authentik-Uid are set, X-Tenant-ID wins.
	mw := Middleware(MiddlewareConfig{
		Resolve: stubResolver("should-not-see-this"),
	})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "direct-tenant-id")
	req.Header.Set("X-Authentik-Uid", "auth-pk-123")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "direct-tenant-id" {
		t.Errorf("priority: got %q, want direct-tenant-id", got)
	}
}

func TestMiddleware_NoHeaders_NoTenant(t *testing.T) {
	mw := Middleware(MiddlewareConfig{})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("no headers: got %q, want no-tenant", got)
	}
}

func TestMiddleware_FakeAuthentikUID_WithoutResolver_NoTenant(t *testing.T) {
	// C2 scenario: attacker sends forged X-Authentik-Uid but no resolver is set.
	// Middleware must NOT trust the header value as a tenant ID directly.
	mw := Middleware(MiddlewareConfig{})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Authentik-Uid", "attacker-forged-uid")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("forged Authentik UID without resolver: got %q, want no-tenant", got)
	}
}

func TestMiddleware_AuthentikUID_UnknownUser_NoAutoProvision(t *testing.T) {
	// Resolver returns empty (unknown user), no auto-provisioner → no tenant.
	mw := Middleware(MiddlewareConfig{
		Resolve: stubResolver(""),
	})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Authentik-Uid", "unknown-user-pk")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "no-tenant" {
		t.Errorf("unknown user: got %q, want no-tenant", got)
	}
}

func TestMiddleware_XTenantID_RequiresServiceToken_WhenConfigured(t *testing.T) {
	// ServiceToken is configured — X-Tenant-ID alone is not trusted.
	mw := Middleware(MiddlewareConfig{ServiceToken: "secret"})(noopHandler())

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
	mw := Middleware(MiddlewareConfig{ServiceToken: "secret"})(noopHandler())

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
	mw := Middleware(MiddlewareConfig{ServiceToken: "secret"})(noopHandler())

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Tenant-ID", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	req.Header.Set("X-Service-Token", "secret")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Body.String(); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("X-Tenant-ID with service token: got %q, want tenant UUID", got)
	}
}

func TestRequire_Returns401_WhenNoTenant(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tid := Require(w, r); tid == "" {
			return
		}
		w.Write([]byte("should-not-reach"))
	})

	mw := Middleware(MiddlewareConfig{})(handler)
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Require without tenant: got %d, want 401", w.Code)
	}
}
