package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"testing"
)

func sortedRoles(rs []Role) []Role {
	out := make([]Role, len(rs))
	copy(out, rs)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestRolesFromGroups_AdminOnly(t *testing.T) {
	got := sortedRoles(RolesFromGroups([]string{"omur-admins"}))
	want := []Role{RoleAdmin}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRolesFromGroups_SupportOnly(t *testing.T) {
	got := sortedRoles(RolesFromGroups([]string{"omur-support"}))
	want := []Role{RoleSupport}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRolesFromGroups_BothGroupsUnion(t *testing.T) {
	got := sortedRoles(RolesFromGroups([]string{"omur-admins", "omur-support"}))
	want := []Role{RoleAdmin, RoleSupport}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRolesFromGroups_UnknownGroupIgnored(t *testing.T) {
	got := RolesFromGroups([]string{"random-group", "tenant-default"})
	if len(got) != 0 {
		t.Fatalf("expected no roles, got %v", got)
	}
}

func TestRolesFromGroups_DuplicatesDeduped(t *testing.T) {
	// A user could (theoretically) appear in the same group twice via parent/child.
	got := sortedRoles(RolesFromGroups([]string{"omur-admins", "omur-admins"}))
	want := []Role{RoleAdmin}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestHasRole_TrueWhenPresent(t *testing.T) {
	ctx := WithRoles(context.Background(), []Role{RoleAdmin})
	if !HasRole(ctx, RoleAdmin) {
		t.Fatal("expected HasRole(admin) = true")
	}
}

func TestHasRole_FalseWhenAbsent(t *testing.T) {
	ctx := WithRoles(context.Background(), []Role{RoleSupport})
	if HasRole(ctx, RoleAdmin) {
		t.Fatal("expected HasRole(admin) = false")
	}
}

func TestHasRole_FalseOnEmptyCtx(t *testing.T) {
	if HasRole(context.Background(), RoleAdmin) {
		t.Fatal("empty context should have no roles")
	}
}

func TestRolesFromContext_ReturnsAttached(t *testing.T) {
	ctx := WithRoles(context.Background(), []Role{RoleAdmin, RoleSupport})
	got := sortedRoles(RolesFromContext(ctx))
	want := []Role{RoleAdmin, RoleSupport}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRolesFromContext_EmptyByDefault(t *testing.T) {
	got := RolesFromContext(context.Background())
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestRequireRole_Allows_WhenRolePresent(t *testing.T) {
	req := httptest.NewRequest("GET", "/admin/x", nil)
	req = req.WithContext(WithRoles(req.Context(), []Role{RoleAdmin}))
	w := httptest.NewRecorder()
	if !RequireRole(w, req, RoleAdmin) {
		t.Fatal("expected RequireRole=true")
	}
	if w.Code != 200 { // recorder default; nothing should be written
		t.Errorf("status: got %d, want 200 (untouched)", w.Code)
	}
}

func TestRolesFromGroups_OmurUsers(t *testing.T) {
	got := RolesFromGroups([]string{"omur-users"})
	want := []Role{RoleUser}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RolesFromGroups(omur-users) = %v, want %v", got, want)
	}
}

func TestRolesFromGroups_MultipleGroups_IncludesUser(t *testing.T) {
	got := RolesFromGroups([]string{"omur-users", "omur-admins"})
	if !slices.Contains(got, RoleUser) || !slices.Contains(got, RoleAdmin) {
		t.Fatalf("got %v, want to include RoleUser and RoleAdmin", got)
	}
}

func TestRequireRole_403_WhenRoleAbsent(t *testing.T) {
	req := httptest.NewRequest("GET", "/admin/x", nil)
	w := httptest.NewRecorder()
	if RequireRole(w, req, RoleAdmin) {
		t.Fatal("expected RequireRole=false")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type: got %q, want application/json", ct)
	}
}
