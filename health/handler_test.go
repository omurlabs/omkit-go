package health_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/omurlabs/omkit-go/health"
)

// startLocalServer spins up an http.Server on a free localhost port and
// returns the port plus a teardown closure. Used by LegacyHealthcheck*
// tests because those functions call http.Get on `localhost:<port>` —
// httptest.NewServer can't drive the localhost-based path resolution.
func startLocalServer(t *testing.T, handler http.HandlerFunc) (int, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(listener)
	}()
	addr := listener.Addr().String()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port parse %q: %v", portStr, err)
	}
	return port, func() {
		_ = srv.Close()
	}
}

func TestHealthHandler(t *testing.T) {
	h := health.Handler("my-service", "1.2.3")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
	if body["service"] != "my-service" {
		t.Errorf("expected service=my-service, got %q", body["service"])
	}
	if body["version"] != "1.2.3" {
		t.Errorf("expected version=1.2.3, got %q", body["version"])
	}
}

func TestReadyHandler_Healthy(t *testing.T) {
	h := health.ReadyHandler(func() error { return nil })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %q", body["status"])
	}
}

func TestReadyHandler_Unhealthy(t *testing.T) {
	h := health.ReadyHandler(func() error { return errors.New("db down") })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "not_ready" {
		t.Errorf("expected status=not_ready, got %q", body["status"])
	}
	if body["error"] != "db down" {
		t.Errorf("expected error=db down, got %q", body["error"])
	}
}

func TestReadyHandlerWithProbes_AllHealthy(t *testing.T) {
	h := health.ReadyHandlerWithProbes("svc", "v1",
		health.Probe{Name: "db", Check: func() error { return nil }},
		health.Probe{Name: "cache", Check: func() error { return nil }},
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("expected checks map, got %T", body["checks"])
	}
	if checks["db"] != "ok" || checks["cache"] != "ok" {
		t.Errorf("expected db=ok cache=ok, got %v", checks)
	}
}

func TestReadyHandlerWithProbes_OnePartialFail(t *testing.T) {
	h := health.ReadyHandlerWithProbes("svc", "v1",
		health.Probe{Name: "db", Check: func() error { return nil }},
		health.Probe{Name: "cache", Check: func() error { return errors.New("conn refused") }},
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "not_ready" {
		t.Errorf("expected not_ready, got %v", body["status"])
	}
	checks := body["checks"].(map[string]any)
	if checks["db"] != "ok" {
		t.Errorf("expected db=ok, got %v", checks["db"])
	}
	if checks["cache"] != "conn refused" {
		t.Errorf("expected cache=conn refused, got %v", checks["cache"])
	}
}

func TestReadyHandlerWithProbes_NoProbes(t *testing.T) {
	h := health.ReadyHandlerWithProbes("svc", "v1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMount_RegistersAllFourPaths(t *testing.T) {
	mux := http.NewServeMux()
	health.Mount(mux, "svc", "v1",
		health.Probe{Name: "db", Check: func() error { return nil }},
	)

	for _, path := range []string{"/health", "/healthz", "/ready", "/readyz"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("path %s: expected 200, got %d", path, rec.Code)
		}
	}
}

// TestLegacyHealthcheck_DelegatesToReadyz verifies the back-compat shim
// keeps probing /readyz so distroless callers that already wired it keep
// working without a re-release.
func TestLegacyHealthcheck_DelegatesToReadyz(t *testing.T) {
	var seenPath string
	port, stop := startLocalServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	defer stop()

	if err := health.LegacyHealthcheck(port); err != nil {
		t.Fatalf("LegacyHealthcheck returned error: %v", err)
	}
	if seenPath != "/readyz" {
		t.Fatalf("LegacyHealthcheck must probe /readyz; got %q", seenPath)
	}
}

// TestLegacyHealthcheckPath_HitsExplicitPath covers the new entry point:
// callers like Synapse probe /ready (dependency-gated) instead of the
// default /readyz.
func TestLegacyHealthcheckPath_HitsExplicitPath(t *testing.T) {
	var seenPath string
	port, stop := startLocalServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	defer stop()

	if err := health.LegacyHealthcheckPath(port, "/ready"); err != nil {
		t.Fatalf("LegacyHealthcheckPath returned error: %v", err)
	}
	if seenPath != "/ready" {
		t.Fatalf("LegacyHealthcheckPath must probe the given path; got %q", seenPath)
	}
}

// TestLegacyHealthcheckPath_PropagatesNon200 covers the readiness-down
// case: a 503 from the probed endpoint must surface as a non-nil error so
// Docker marks the container unhealthy.
func TestLegacyHealthcheckPath_PropagatesNon200(t *testing.T) {
	port, stop := startLocalServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer stop()

	err := health.LegacyHealthcheckPath(port, "/ready")
	if err == nil {
		t.Fatalf("expected error when probe returns 503; got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to mention 503; got %v", err)
	}
}

// TestLegacyHealthcheckPath_RejectsMalformedPath enforces the leading-
// slash contract — without it the URL composes incorrectly (host gets
// merged into the path) and the failure mode is silent.
func TestLegacyHealthcheckPath_RejectsMalformedPath(t *testing.T) {
	for _, badPath := range []string{"", "ready", "readyz"} {
		err := health.LegacyHealthcheckPath(8000, badPath)
		if err == nil {
			t.Errorf("expected error for malformed path %q; got nil", badPath)
		}
	}
}

func TestMount_LivenessSurvivesFailingProbe(t *testing.T) {
	mux := http.NewServeMux()
	health.Mount(mux, "svc", "v1",
		health.Probe{Name: "db", Check: func() error { return errors.New("down") }},
	)

	for _, path := range []string{"/health", "/healthz"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("liveness %s should stay 200 even when probe fails, got %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/ready", "/readyz"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("readiness %s should be 503 with failing probe, got %d", path, rec.Code)
		}
	}
}
