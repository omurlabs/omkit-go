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

func TestHTTPMiddlewareWithExclusions_SkipsExcluded(t *testing.T) {
	wrapped := metrics.HTTPMiddlewareWithExclusions("excl-svc", "/metrics", "/health")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	// Hit excluded paths — these should NOT increment metrics
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// Hit a non-excluded path — this SHOULD increment metrics
	resp, err = http.Get(srv.URL + "/foo")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Scrape metrics output
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	if !strings.Contains(body, `service="excl-svc"`) {
		t.Errorf("expected service label for /foo, got: %s", body)
	}

	// http_requests_total{service="excl-svc",method="GET",code="200"} should be exactly 1.
	// If /metrics and /health were counted, it would be 3.
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, `http_requests_total{service="excl-svc"`) {
			// Expect the counter value to be 1 (single non-excluded GET /foo).
			if !strings.HasSuffix(strings.TrimSpace(line), " 1") {
				t.Errorf("expected counter value 1 for excl-svc, got line: %q", line)
			}
		}
	}
}

func TestDefaultMetricsExclusions_Contents(t *testing.T) {
	expected := map[string]bool{"/metrics": true, "/health": true, "/healthz": true, "/ready": true}
	if len(metrics.DefaultMetricsExclusions) != len(expected) {
		t.Errorf("DefaultMetricsExclusions length = %d, want %d", len(metrics.DefaultMetricsExclusions), len(expected))
	}
	for _, p := range metrics.DefaultMetricsExclusions {
		if !expected[p] {
			t.Errorf("unexpected path in DefaultMetricsExclusions: %s", p)
		}
	}
}
