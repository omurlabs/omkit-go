// flag.go — flag module.
//
// exports: Flag | ValidateRoles
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package featureflags provides a shared role-scoped feature-flag primitive.
// A flag is {Enabled: bool, Roles: []auth.Role}; a caller is Allowed iff the
// flag is enabled AND the caller's roles intersect the allowlist. Unknown
// flag denies; empty roles slice denies everyone.
//
// Typical deployment puts enforcement at the gateway (all user HTTP enters
// there and internal services trust the gateway). Other services can adopt
// Store + Allowed on demand if they grow user-exposed HTTP.
package featureflags

import "github.com/omurlabs/omkit-go/auth"

// Flag is the normalized shape of one feature flag.
type Flag struct {
	Enabled bool
	Roles   []auth.Role
}

// ValidateRoles reports the first role string that is not in the known catalog
// (RoleAdmin, RoleSupport, RoleUser). Returns "" when all roles are valid.
// Empty input is valid (deny-all is a legitimate state).
func ValidateRoles(roles []string) string {
	known := map[string]struct{}{
		string(auth.RoleAdmin):   {},
		string(auth.RoleSupport): {},
		string(auth.RoleUser):    {},
	}
	for _, r := range roles {
		if _, ok := known[r]; !ok {
			return r
		}
	}
	return ""
}
