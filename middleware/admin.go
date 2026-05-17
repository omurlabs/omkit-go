// admin.go — admin module.
//
// exports: AdminConfig | AdminMiddleware
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/omurlabs/omkit-go/auth"
)

// AdminConfig configures the admin role-mapping middleware.
type AdminConfig struct {
	// ServiceToken is the cluster-wide service token. When non-empty, requests
	// that present a matching X-Service-Token AND carry NO X-Auth-Request-User
	// header are treated as service-to-service and granted full admin powers.
	ServiceToken string
}

// AdminMiddleware parses the Zitadel groups header (pipe-separated, per
// Caddy forward_auth) into a role list and attaches it to the request
// context. Handlers gate on auth.RequireRole(...).
//
// Service-to-service calls (X-Service-Token match, no X-Auth-Request-User)
// are granted the full role set [admin, support]. Browser requests, even
// though caddy injects X-Service-Token, also carry X-Auth-Request-User and
// therefore only get header-derived roles.
//
// The middleware ALWAYS runs — it never rejects. /admin/* path-level gating
// happens via auth.RequireRole inside each handler (or via a wrapper
// registered when the prefix is mounted).
//
// SECURITY: Trust in the groups header depends on Caddy's forward_auth
// block stripping any client-supplied values BEFORE injecting the
// IdP-validated ones. If a request reaches this middleware without going
// through Caddy forward_auth (e.g. a service is briefly exposed on a
// non-Caddy port, or a peer on the backend network bypasses the proxy),
// the headers are attacker-controlled. BearerAuth's X-Service-Token
// requirement is the only defense in that scenario. Do NOT expose Spine
// outside the Caddy/proxy boundary.
func AdminMiddleware(cfg AdminConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roles := computeRoles(r, cfg)
			if len(roles) > 0 {
				ctx := auth.WithRoles(r.Context(), roles)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func computeRoles(r *http.Request, cfg AdminConfig) []auth.Role {
	// Service-token short-circuit: requires no X-Auth-Request-User header to
	// avoid misclassifying browser requests that arrive with Caddy-injected
	// X-Service-Token but also carry a user header.
	if cfg.ServiceToken != "" &&
		r.Header.Get("X-Service-Token") == cfg.ServiceToken &&
		r.Header.Get("X-Auth-Request-User") == "" {
		return []auth.Role{auth.RoleAdmin, auth.RoleSupport}
	}

	groups := parseGroups(r.Header.Get("X-Auth-Request-Groups"))
	return auth.RolesFromGroups(groups)
}

// parseGroups splits the pipe-separated groups header value and trims
// whitespace from each entry. Empty entries are dropped.
//
// Zitadel's `urn:zitadel:iam:org:project:roles` claim is a nested JSON
// object keyed on role name. oauth2-proxy v7.15+ does not flatten it,
// so the entire JSON arrives as a single header value. Detect that shape
// and extract top-level keys as the role list.
func parseGroups(raw string) []string {
	if raw == "" {
		return nil
	}
	if trimmed := strings.TrimSpace(raw); strings.HasPrefix(trimmed, "{") {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
			out := make([]string, 0, len(obj))
			for k := range obj {
				if k = strings.TrimSpace(k); k != "" {
					out = append(out, k)
				}
			}
			return out
		}
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
