package featureflags

import (
	"context"
	"testing"

	"github.com/omurlabs/omkit-go/auth"
)

func TestAllowed_FlagDisabled_ReturnsFalse(t *testing.T) {
	store := NewStaticStore(map[string]Flag{
		"flag.foo": {Enabled: false, Roles: []auth.Role{auth.RoleUser}},
	})
	ctx := auth.WithRoles(context.Background(), []auth.Role{auth.RoleUser})
	if Allowed(ctx, store, "flag.foo") {
		t.Fatal("disabled flag should not be Allowed")
	}
}

func TestAllowed_RoleNotInAllowlist_ReturnsFalse(t *testing.T) {
	store := NewStaticStore(map[string]Flag{
		"flag.foo": {Enabled: true, Roles: []auth.Role{auth.RoleAdmin}},
	})
	ctx := auth.WithRoles(context.Background(), []auth.Role{auth.RoleUser})
	if Allowed(ctx, store, "flag.foo") {
		t.Fatal("user should not access admin-only flag")
	}
}

func TestAllowed_RoleInAllowlist_ReturnsTrue(t *testing.T) {
	store := NewStaticStore(map[string]Flag{
		"flag.foo": {Enabled: true, Roles: []auth.Role{auth.RoleAdmin, auth.RoleUser}},
	})
	ctx := auth.WithRoles(context.Background(), []auth.Role{auth.RoleUser})
	if !Allowed(ctx, store, "flag.foo") {
		t.Fatal("user in allowlist should be Allowed")
	}
}

func TestAllowed_EmptyRoles_DeniesEveryone(t *testing.T) {
	store := NewStaticStore(map[string]Flag{
		"flag.foo": {Enabled: true, Roles: []auth.Role{}},
	})
	for _, r := range []auth.Role{auth.RoleAdmin, auth.RoleSupport, auth.RoleUser} {
		ctx := auth.WithRoles(context.Background(), []auth.Role{r})
		if Allowed(ctx, store, "flag.foo") {
			t.Fatalf("empty roles must deny %s", r)
		}
	}
}

func TestAllowed_UnknownFlag_DeniesByDefault(t *testing.T) {
	store := NewStaticStore(map[string]Flag{})
	ctx := auth.WithRoles(context.Background(), []auth.Role{auth.RoleAdmin})
	if Allowed(ctx, store, "flag.does.not.exist") {
		t.Fatal("unknown flag must be denied")
	}
}

func TestAllowed_NoRolesOnCaller_DeniesEveryNonEmptyFlag(t *testing.T) {
	store := NewStaticStore(map[string]Flag{
		"flag.foo": {Enabled: true, Roles: []auth.Role{auth.RoleUser}},
	})
	if Allowed(context.Background(), store, "flag.foo") {
		t.Fatal("caller with no roles must not pass")
	}
}
