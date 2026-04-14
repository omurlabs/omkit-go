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

// BearerAuth validates Authorization: Bearer <token> against expectedToken.
// Skips auth for health/ready endpoints.
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
		if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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
