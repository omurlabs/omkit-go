package middleware

import (
	"net/http"
	"strings"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/auth"
)

// AdminConfig configures the admin role-mapping middleware.
type AdminConfig struct {
	// ServiceToken is the cluster-wide service token. When non-empty, requests
	// that present a matching X-Service-Token AND have NO X-Authentik-Uid are
	// treated as service-to-service calls and granted full admin powers
	// (matches the existing trust boundary documented on BearerAuth).
	ServiceToken string
}

// AdminMiddleware parses X-Authentik-Groups (pipe-separated, per Caddy
// forward_auth) into a role list and attaches it to the request context.
// Handlers gate on auth.RequireRole(...).
//
// Service-to-service calls (X-Service-Token match, no X-Authentik-Uid) are
// granted the full role set [admin, support]. Browser requests, even though
// caddy injects X-Service-Token, also carry X-Authentik-Uid and therefore
// only get header-derived roles.
//
// The middleware ALWAYS runs — it never rejects. /admin/* path-level gating
// happens via auth.RequireRole inside each handler (or via a wrapper
// registered when the prefix is mounted).
//
// SECURITY: Trust in X-Authentik-Groups depends on Caddy's forward_auth
// block stripping any client-supplied X-Authentik-* headers BEFORE injecting
// the Authentik-validated values. If a request reaches this middleware
// without going through Caddy forward_auth (e.g. a service is briefly
// exposed on a non-Caddy port, or a peer on the backend network bypasses
// the proxy), the headers are attacker-controlled. BearerAuth's
// X-Service-Token requirement is the only defense in that scenario.
// Do NOT expose Spine outside the Caddy/proxy boundary.
func AdminMiddleware(cfg AdminConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roles := computeRoles(r, cfg.ServiceToken)
			if len(roles) > 0 {
				ctx := auth.WithRoles(r.Context(), roles)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func computeRoles(r *http.Request, serviceToken string) []auth.Role {
	// Service-token short-circuit. Distinguish browser traffic (which also
	// carries the service token courtesy of caddy) by the presence of
	// X-Authentik-Uid: a browser request always has it, a peer-call never does.
	if serviceToken != "" &&
		r.Header.Get("X-Service-Token") == serviceToken &&
		r.Header.Get("X-Authentik-Uid") == "" {
		return []auth.Role{auth.RoleAdmin, auth.RoleSupport}
	}

	groups := parseGroups(r.Header.Get("X-Authentik-Groups"))
	return auth.RolesFromGroups(groups)
}

// parseGroups splits the pipe-separated X-Authentik-Groups header value
// and trims whitespace from each entry. Empty entries are dropped.
func parseGroups(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
