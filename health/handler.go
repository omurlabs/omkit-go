// Package health provides standard HTTP health check handlers.
package health

import (
	"encoding/json"
	"net/http"
)

// Handler returns an http.Handler that responds with service name and version.
func Handler(service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": service,
			"version": version,
		})
	})
}

// ReadyHandler returns an http.Handler that calls check() and responds 200 or 503.
func ReadyHandler(check func() error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := check(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "not_ready",
				"error":  err.Error(),
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	})
}
