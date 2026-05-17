// metrics.go — metrics module.
//
// exports: Handler | Counter | Histogram | Gauge | WriteHeader | HTTPMiddleware | DefaultMetricsExclusions | HTTPMiddlewareWithExclusions
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package metrics provides Prometheus instrumentation helpers.
//
// Usage:
//
//	metrics.HTTPMiddleware("my-service") wraps an http.Handler and records
//	http_requests_total + http_request_duration_seconds.
//	metrics.Handler() returns the /metrics endpoint handler.
//	metrics.Counter / Histogram / Gauge are factories using promauto.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the /metrics endpoint handler.
func Handler() http.Handler { return promhttp.Handler() }

// Counter creates and registers a CounterVec with the default registry.
func Counter(name, help string, labels ...string) *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
}

// Histogram creates and registers a HistogramVec with default duration buckets.
func Histogram(name, help string, labels ...string) *prometheus.HistogramVec {
	return promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Help:    help,
		Buckets: prometheus.DefBuckets,
	}, labels)
}

// Gauge creates and registers a GaugeVec with the default registry.
func Gauge(name, help string, labels ...string) *prometheus.GaugeVec {
	return promauto.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
}

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"service", "method", "code"})
	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"service", "method"})
)

// statusRecorder captures the response status for metric labelling.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// HTTPMiddleware records http_requests_total and http_request_duration_seconds
// for each request handled by the wrapped Handler. This is equivalent to
// HTTPMiddlewareWithExclusions(serviceName) with no exclusions.
func HTTPMiddleware(serviceName string) func(http.Handler) http.Handler {
	return HTTPMiddlewareWithExclusions(serviceName)
}

// DefaultMetricsExclusions are paths that should not be counted as application
// HTTP traffic — typically scrape and probe endpoints.
var DefaultMetricsExclusions = []string{"/metrics", "/health", "/healthz", "/ready"}

// HTTPMiddlewareWithExclusions is HTTPMiddleware that skips recording for the
// given exact paths. Pass DefaultMetricsExclusions for the conventional set.
func HTTPMiddlewareWithExclusions(serviceName string, excludePaths ...string) func(http.Handler) http.Handler {
	excl := make(map[string]struct{}, len(excludePaths))
	for _, p := range excludePaths {
		excl[p] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := excl[r.URL.Path]; skip {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			httpRequestsTotal.WithLabelValues(serviceName, r.Method, strconv.Itoa(rec.status)).Inc()
			httpRequestDuration.WithLabelValues(serviceName, r.Method).Observe(time.Since(start).Seconds())
		})
	}
}
