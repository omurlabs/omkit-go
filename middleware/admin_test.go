package middleware

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/auth"
)

// captureHandler returns the roles attached to the request context.
func captureHandler(out *[]auth.Role) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*out = auth.RolesFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func sortedRoles(rs []auth.Role) []auth.Role {
	out := make([]auth.Role, len(rs))
	copy(out, rs)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestAdminMiddleware_ParsesAdminGroup(t *testing.T) {
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Auth-Request-Groups", "tenant-default|omur-admin|omur-user")
	req.Header.Set("X-Service-Token", "secret") // browser path: caddy injects this
	// But this request was forwarded with X-Auth-Request-User (browser flow);
	// X-Tenant-ID is NOT set, so service-token short-circuit must NOT trigger.
	req.Header.Set("X-Auth-Request-User", "user-sub-42")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	// omur-user maps to RoleUser (role-scoped feature flags); admin+user
	// is the correct union for an admin who also sits in the plain-user group.
	want := []auth.Role{auth.RoleAdmin, auth.RoleUser}
	if !reflect.DeepEqual(sortedRoles(got), want) {
		t.Errorf("roles: got %v, want %v", got, want)
	}
}

func TestAdminMiddleware_ParsesSupportGroup(t *testing.T) {
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Auth-Request-Groups", "  omur-support  ") // whitespace tolerated
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Auth-Request-User", "user-sub-42")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !reflect.DeepEqual(sortedRoles(got), []auth.Role{auth.RoleSupport}) {
		t.Errorf("roles: got %v, want [support]", got)
	}
}

func TestAdminMiddleware_NoGroupsHeader_NoRoles(t *testing.T) {
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Auth-Request-User", "user-sub-42")
	// No X-Auth-Request-Groups.

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if len(got) != 0 {
		t.Errorf("expected no roles, got %v", got)
	}
}

func TestAdminMiddleware_UnknownGroupsIgnored(t *testing.T) {
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	// tenant-default is unknown; garbage-group is unknown; only those should
	// be ignored. We deliberately do NOT include omur-user here because
	// that group now maps to a real role.
	req.Header.Set("X-Auth-Request-Groups", "tenant-default|garbage-group")
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Auth-Request-User", "user-sub-42")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if len(got) != 0 {
		t.Errorf("expected no roles for unknown groups, got %v", got)
	}
}

func TestAdminMiddleware_ServiceTokenShortCircuit_GrantsAdmin(t *testing.T) {
	// Internal service-to-service call: presents X-Service-Token + X-Tenant-ID,
	// no X-Auth-Request-User. Must be granted full admin.
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Tenant-ID", "11111111-2222-3333-4444-555555555555")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	want := []auth.Role{auth.RoleAdmin, auth.RoleSupport}
	if !reflect.DeepEqual(sortedRoles(got), want) {
		t.Errorf("service-token short-circuit: got %v, want %v", got, want)
	}
}

func TestAdminMiddleware_BrowserRequest_DoesNotShortCircuit(t *testing.T) {
	// Browser request: caddy injects X-Service-Token but ALSO X-Auth-Request-User.
	// The presence of X-Auth-Request-User means this is a user request — only
	// header-derived roles apply, no admin grant from the service token alone.
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Auth-Request-User", "user-sub-42")
	req.Header.Set("X-Auth-Request-Groups", "tenant-default") // not an admin group
	// no X-Tenant-ID

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if len(got) != 0 {
		t.Errorf("expected no roles for plain user, got %v", got)
	}
}

func TestAdminMiddleware_WrongServiceToken_NoShortCircuit(t *testing.T) {
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Service-Token", "wrong")
	req.Header.Set("X-Tenant-ID", "11111111-2222-3333-4444-555555555555")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if len(got) != 0 {
		t.Errorf("expected no roles for wrong service token, got %v", got)
	}
}

func TestAdminMiddleware_EmptyServiceTokenConfig_DisablesShortCircuit(t *testing.T) {
	// Dev mode: cfg.TenantToken == "". The middleware must still parse groups,
	// but the service-token short-circuit must not fire (no token to compare).
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: ""})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Service-Token", "anything")
	req.Header.Set("X-Tenant-ID", "11111111-2222-3333-4444-555555555555")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if len(got) != 0 {
		t.Errorf("empty service-token config: got %v, want no roles", got)
	}
}
