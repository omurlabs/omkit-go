// handler.go — health and readiness HTTP handlers.
//
// exports: Handler | ReadyHandler | Probe | ReadyHandlerWithProbes | Mount | LegacyHealthcheck
// used_by: services/pulse/main.go | services/spine/main.go | services/cortex/log.go | services/synapse/main.go | services/reflex/main.go | services/solid-sync/handler/health.go
// rules:   Liveness handlers must never call external dependencies — they only confirm the process is running. Readiness handlers may call dependencies but must complete fast (under 3s); a slow dependency must surface as not_ready, not as a hung probe. Both /health and /healthz must always return 200 once the process accepts connections, even when readiness is failing.
// agent:   claude-opus-4-7 | anthropic | 2026-05-03 | track-9-health-ready-audit | added Probe + Mount + LegacyHealthcheck for /healthz + /readyz aliases
// message:

// Package health provides standard HTTP health check handlers for Omur services.
//
// Two paths convey two different signals:
//
//   - Liveness (/health, /healthz): process is up and accepting connections.
//     Returns 200 unconditionally. Liveness must never depend on external
//     services — orchestrators interpret a non-200 as "restart this pod".
//
//   - Readiness (/ready, /readyz): all required dependencies are reachable
//     and the service is willing to handle traffic. Returns 200 when every
//     probe succeeds, 503 with details when any fails. Orchestrators
//     interpret a non-200 as "do not route traffic here yet" — the pod
//     stays up.
package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Probe is a named readiness check. The function must return nil when the
// dependency is reachable and a descriptive error otherwise. Probes must
// complete quickly — callers should wrap dependency calls with their own
// short context timeout (typically 1–3s).
type Probe struct {
	Name  string
	Check func() error
}

// Handler returns an http.Handler that responds 200 with service name and
// version. Use it for liveness paths (/health, /healthz). It does not
// consult any dependency.
func Handler(service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": service,
			"version": version,
		})
	})
}

// ReadyHandler returns an http.Handler that calls check() and responds 200
// or 503. Retained for callers that have a single combined readiness check.
// Prefer ReadyHandlerWithProbes for new code so failing components are
// reported individually.
func ReadyHandler(check func() error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := check(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "not_ready",
				"error":  err.Error(),
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	})
}

// ReadyHandlerWithProbes returns an http.Handler that runs each probe and
// reports each component's status. Returns 200 only when every probe
// returns nil; 503 with a per-probe map of error messages otherwise.
func ReadyHandlerWithProbes(service, version string, probes ...Probe) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		checks := make(map[string]string, len(probes))
		var failed []string
		for _, p := range probes {
			if p.Check == nil {
				continue
			}
			if err := p.Check(); err != nil {
				checks[p.Name] = err.Error()
				failed = append(failed, p.Name)
			} else {
				checks[p.Name] = "ok"
			}
		}

		body := map[string]any{
			"service": service,
			"version": version,
			"checks":  checks,
		}
		if len(failed) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			body["status"] = "not_ready"
			body["failed"] = strings.Join(failed, ",")
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		w.WriteHeader(http.StatusOK)
		body["status"] = "ready"
		_ = json.NewEncoder(w).Encode(body)
	})
}

// Muxer is the subset of *http.ServeMux that Mount needs. Accepting an
// interface keeps the SDK reusable for projects that wrap their mux.
type Muxer interface {
	Handle(pattern string, handler http.Handler)
}

// Mount registers all four standard paths on the given mux:
//
//	GET /health   GET /healthz   →  liveness (200, no deps)
//	GET /ready    GET /readyz    →  readiness (probes; 200 or 503)
//
// Every Omur service that exposes HTTP should call this. Pass zero probes
// for services that have no external dependencies to gate; they report
// "ready" immediately.
func Mount(mux Muxer, service, version string, probes ...Probe) {
	live := Handler(service, version)
	ready := ReadyHandlerWithProbes(service, version, probes...)
	mux.Handle("GET /health", live)
	mux.Handle("GET /healthz", live)
	mux.Handle("GET /ready", ready)
	mux.Handle("GET /readyz", ready)
}

// LegacyHealthcheck is the body of a `<service> -healthcheck` Docker
// subcommand: it hits /readyz on localhost at the given port and returns a
// non-nil error when the service is not ready. Wire it into main() before
// normal startup so distroless images can run a CMD-based HEALTHCHECK
// without curl/wget on the runtime image.
func LegacyHealthcheck(port int) error {
	url := fmt.Sprintf("http://localhost:%d/readyz", port)
	resp, err := http.Get(url) //nolint:gosec,noctx // localhost-only readiness probe
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz returned %d", resp.StatusCode)
	}
	return nil
}
