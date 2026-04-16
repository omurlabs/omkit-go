package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/metrics"
)

func TestHandler_ExposesMetrics(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Errorf("expected go_goroutines metric in output")
	}
}

func TestHTTPMiddleware_RecordsRequest(t *testing.T) {
	wrapped := metrics.HTTPMiddleware("test-svc")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/foo")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "http_requests_total") {
		t.Errorf("expected http_requests_total metric, got: %s", body)
	}
	if !strings.Contains(body, `service="test-svc"`) {
		t.Errorf("expected service label, got: %s", body)
	}
}

func TestCounter_Factory(t *testing.T) {
	c := metrics.Counter("test_counter_total", "test", "label")
	c.WithLabelValues("a").Inc()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `test_counter_total{label="a"} 1`) {
		t.Errorf("expected counter value, got: %s", rec.Body.String())
	}
}

func TestHistogram_Factory(t *testing.T) {
	h := metrics.Histogram("test_histogram_seconds", "test", "label")
	h.WithLabelValues("a").Observe(0.5)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `test_histogram_seconds_count{label="a"} 1`) {
		t.Errorf("expected histogram count, got: %s", rec.Body.String())
	}
}

func TestGauge_Factory(t *testing.T) {
	g := metrics.Gauge("test_gauge", "test", "label")
	g.WithLabelValues("a").Set(42)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `test_gauge{label="a"} 42`) {
		t.Errorf("expected gauge value, got: %s", rec.Body.String())
	}
}
