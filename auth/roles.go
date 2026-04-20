// Package auth provides the role catalog and authorization helpers shared
// across Omur Go services. Identity comes from Authentik (forward_auth
// headers); roles are computed from group membership using a hardcoded map.
//
// Adding a role requires a code change — that's the right friction for a
// security-sensitive list. Group names live in Terraform (infra/authentik).
package auth

import (
	"context"
	"net/http"
)

// Role is one of a fixed set of authorization roles.
type Role string

const (
	RoleAdmin   Role = "admin"
	RoleSupport Role = "support"
)

// groupToRoles is the source of truth for Authentik-group → role mapping.
// Keys MUST match the `name` field of the corresponding authentik_group
// resource in infra/authentik/groups.tf.
var groupToRoles = map[string][]Role{
	"omur-admins":  {RoleAdmin},
	"omur-support": {RoleSupport},
}

// RolesFromGroups returns the deduplicated union of roles granted by the
// given Authentik group memberships. Unknown groups are silently ignored.
// Order of the returned slice is not specified.
func RolesFromGroups(groups []string) []Role {
	if len(groups) == 0 {
		return nil
	}
	seen := make(map[Role]struct{}, 2)
	var out []Role
	for _, g := range groups {
		for _, r := range groupToRoles[g] {
			if _, dup := seen[r]; dup {
				continue
			}
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

type ctxKey struct{}

// WithRoles attaches the given roles to ctx. Used by the admin middleware.
func WithRoles(ctx context.Context, roles []Role) context.Context {
	return context.WithValue(ctx, ctxKey{}, roles)
}

// RolesFromContext returns the roles attached by WithRoles, or nil.
func RolesFromContext(ctx context.Context) []Role {
	if v, ok := ctx.Value(ctxKey{}).([]Role); ok {
		return v
	}
	return nil
}

// HasRole reports whether ctx carries the given role.
func HasRole(ctx context.Context, want Role) bool {
	for _, r := range RolesFromContext(ctx) {
		if r == want {
			return true
		}
	}
	return false
}

// RequireRole writes a 403 JSON response and returns false if ctx does not
// carry the required role. Handlers gate on this:
//
//	if !auth.RequireRole(w, r, auth.RoleAdmin) { return }
func RequireRole(w http.ResponseWriter, r *http.Request, want Role) bool {
	if HasRole(r.Context(), want) {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden: missing required role"}`))
	return false
}
