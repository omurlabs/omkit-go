// roles.go — roles module.
//
// exports: Role | RoleAdmin | RoleSupport | RoleUser | RolesFromGroups | WithRoles | RolesFromContext | HasRole | RequireRole
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package auth provides the role catalog and authorization helpers shared
// across Go services. Identity comes from Zitadel (forward_auth headers
// via oauth2-proxy); roles are computed from group membership using a
// hardcoded map.
//
// Adding a role requires a code change — that's the right friction for a
// security-sensitive list. Role keys live in Terraform (infra/zitadel/roles.tf).
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
	// RoleUser covers two distinct actors that land in the same audit
	// column: (a) omur-user role members, for role-gated admin/settings
	// features; (b) self-service auth-path events (signup, login, logout,
	// credential add/delete) where the account owner IS the actor — no
	// IdP role is required for that flow because the auth handlers
	// emit the entry directly. admin_audit_log's CHECK constraint on
	// `role` must include 'user' for these rows to persist.
	RoleUser Role = "user"
)

// groupToRoles is the source of truth for Zitadel-role-key → Role mapping.
// Keys MUST match the `role_key` field of the corresponding
// zitadel_project_role resource in infra/zitadel/roles.tf (singular).
var groupToRoles = map[string][]Role{
	"omur-admin":   {RoleAdmin},
	"omur-support": {RoleSupport},
	"omur-user":    {RoleUser},
}

// RolesFromGroups returns the deduplicated union of roles granted by the
// given Zitadel role-key memberships. Unknown keys are silently ignored.
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
