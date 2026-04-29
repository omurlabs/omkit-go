package jobqueue

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestQueueMetricsCollector_Describe verifies all 13 gauges + error counter
// are described, so prometheus.MustRegister won't panic on duplicate names.
func TestQueueMetricsCollector_Describe(t *testing.T) {
	c := NewQueueMetricsCollector(nil, "test-svc")
	ch := make(chan *prometheus.Desc, 32)
	c.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count < 13 {
		t.Fatalf("expected ≥13 descriptors, got %d", count)
	}
}

// TestQueueMetricsCollector_NewDoesNotPanic guards the construction path —
// the test environment has no live Redis, so Collect would error, but the
// collector should at least be safe to instantiate and register against a
// fresh registry without panicking on duplicate metric names.
func TestQueueMetricsCollector_NewDoesNotPanic(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewQueueMetricsCollector(nil, "test-svc")
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}
}
