// allowed.go — allowed module.
//
// exports: Allowed
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package featureflags

import (
	"context"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/auth"
)

// Allowed reports whether the caller in ctx is permitted to see the feature
// guarded by key. It is a free function (not a Store method) so handler tests
// can pass a StaticStore directly without synthesizing context-aware mocks.
//
// Semantics:
//   - Unknown flag → false (deny-by-default; a typo fails safe).
//   - flag.Enabled == false → false.
//   - callerRoles ∩ flag.Roles == ∅ → false (empty flag.Roles denies everyone).
//   - Otherwise true.
func Allowed(ctx context.Context, store Store, key string) bool {
	flag, ok := store.Get(key)
	if !ok || !flag.Enabled {
		return false
	}
	callerRoles := auth.RolesFromContext(ctx)
	if len(callerRoles) == 0 || len(flag.Roles) == 0 {
		return false
	}
	for _, cr := range callerRoles {
		for _, fr := range flag.Roles {
			if cr == fr {
				return true
			}
		}
	}
	return false
}
