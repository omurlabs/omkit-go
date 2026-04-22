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
	req.Header.Set("X-Authentik-Groups", "tenant-default|omur-admins|omur-users")
	req.Header.Set("X-Service-Token", "secret") // browser path: caddy injects this
	// But this request was forwarded with Authentik-Uid (browser flow); X-Tenant-ID
	// is NOT set, so service-token short-circuit must NOT trigger.
	req.Header.Set("X-Authentik-Uid", "user-pk-42")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	// omur-users now maps to RoleUser (role-scoped feature flags); admin+user
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
	req.Header.Set("X-Authentik-Groups", "  omur-support  ") // whitespace tolerated
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Authentik-Uid", "user-pk-42")

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
	req.Header.Set("X-Authentik-Uid", "user-pk-42")
	// No X-Authentik-Groups.

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
	// be ignored. We deliberately do NOT include omur-users here because
	// that group now maps to a real role.
	req.Header.Set("X-Authentik-Groups", "tenant-default|garbage-group")
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Authentik-Uid", "user-pk-42")

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if len(got) != 0 {
		t.Errorf("expected no roles for unknown groups, got %v", got)
	}
}

func TestAdminMiddleware_ServiceTokenShortCircuit_GrantsAdmin(t *testing.T) {
	// Internal service-to-service call: presents X-Service-Token + X-Tenant-ID,
	// no X-Authentik-Uid. Must be granted full admin.
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
	// Browser request: caddy injects X-Service-Token but ALSO X-Authentik-Uid.
	// The presence of X-Authentik-Uid means this is a user request — only
	// header-derived roles apply, no admin grant from the service token alone.
	var got []auth.Role
	mw := AdminMiddleware(AdminConfig{ServiceToken: "secret"})(captureHandler(&got))

	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Service-Token", "secret")
	req.Header.Set("X-Authentik-Uid", "user-pk-42")
	req.Header.Set("X-Authentik-Groups", "tenant-default") // not an admin group
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

func TestAdminMiddlewareZitadelHeaders(t *testing.T) {
	cfg := AdminConfig{
		ServiceToken: "svc-token",
		IDPMode:      "zitadel",
	}
	mw := AdminMiddleware(cfg)

	// Browser request under Zitadel — carries both X-Service-Token (Caddy-injected)
	// and X-Auth-Request-User. Must NOT be treated as service-to-service.
	req := httptest.NewRequest("GET", "/admin/x", nil)
	req.Header.Set("X-Service-Token", "svc-token")
	req.Header.Set("X-Auth-Request-User", "198261369861120001")
	// NOTE: plan §Task 3.6 text used "omur-admin|omur-user" (singular) but
	// the existing role catalog in packages/omur-go-sdk/auth/roles.go only
	// recognizes "omur-admins"/"omur-users" (plural). Group-name rename is
	// infra work (spec §Roles) that will land with Zitadel role provisioning.
	// Using plural keys here lets the test exercise the IDPMode header
	// dispatch without depending on the deferred rename.
	req.Header.Set("X-Auth-Request-Groups", "omur-admins|omur-users")

	var gotRoles []auth.Role
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRoles = auth.RolesFromContext(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)

	// Expect header-derived roles (omur-admin only from the groups), NOT the
	// service-token short-circuit [admin, support] pair.
	if containsRole(gotRoles, auth.RoleSupport) {
		t.Errorf("browser request under zitadel should not get service-token roles: %v", gotRoles)
	}
	if !containsRole(gotRoles, auth.RoleAdmin) {
		t.Errorf("expected RoleAdmin from groups header, got %v", gotRoles)
	}
}

func containsRole(rs []auth.Role, want auth.Role) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}
