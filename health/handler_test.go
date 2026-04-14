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
