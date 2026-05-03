package health_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/health"
)

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
