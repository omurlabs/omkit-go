// Package middleware provides common HTTP middleware for Omur Go services.
package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// CORS wraps a handler with CORS headers based on allowed origins.
func CORS(origins []string, next http.Handler) http.Handler {
	originSet := make(map[string]bool, len(origins))
	var allowAll bool
	for _, o := range origins {
		if o == "*" {
			allowAll = true
		}
		originSet[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowAll || originSet[origin] || matchWildcard(origins, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, X-Tenant-ID, X-Service-Token")
			w.Header().Set("Access-Control-Max-Age", "3600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func matchWildcard(patterns []string, origin string) bool {
	for _, p := range patterns {
		if strings.HasPrefix(p, "*.") {
			suffix := p[1:] // e.g. ".omur.local"
			if strings.HasSuffix(origin, suffix) || strings.Contains(origin, "://"+p[2:]) {
				return true
			}
		}
	}
	return false
}

// BearerAuth validates a request using one of:
//   - Authorization: Bearer <token>            (external client with API token)
//   - X-Service-Token: <token>                 (internal service-to-service, OR
//                                               Caddy-injected on forward_auth paths)
//
// Caddy MUST inject X-Service-Token on every reverse_proxy to a Go service; this
// is what authenticates browser-session traffic that uses Authentik forward_auth.
// X-Authentik-Uid alone is NOT sufficient: a peer on the backend Docker network
// could otherwise forge it to bypass auth. Tenant resolution from X-Authentik-Uid
// happens downstream in tenant.Middleware.
//
// Skips auth for health/ready endpoints.
//
// If token is empty, BearerAuth is a no-op (dev mode). Production services
// SHOULD use MustBearerAuth instead so an unset OMUR_TENANT_TOKEN fails at
// startup rather than silently disabling authentication at runtime.
func BearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/health" || path == "/healthz" || path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") && auth[7:] == token {
			next.ServeHTTP(w, r)
			return
		}
		if svcToken := r.Header.Get("X-Service-Token"); svcToken != "" && svcToken == token {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
}

// MustBearerAuth is BearerAuth but panics when token is empty. Use this in
// service main() so a misconfigured deployment fails at startup rather than
// silently running with authentication disabled. Services should prefer this
// helper over BearerAuth in production paths.
func MustBearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		panic("middleware: MustBearerAuth requires a non-empty token (set OMUR_TENANT_TOKEN)")
	}
	return BearerAuth(token, next)
}

// RequestLog logs each request with method, path, status, and duration.
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		slog.Info("http.request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
