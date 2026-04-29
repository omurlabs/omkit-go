package jobqueue

import (
	"sync"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
)

// QueueMetricsCollector adapts asynq.Inspector to a prometheus.Collector.
//
// On every scrape, it calls Inspector.Queues() then Inspector.GetQueueInfo(q)
// for each queue and emits gauges. Failures from the Inspector are counted
// in asynq_inspector_errors_total but do not crash the scrape — the existing
// gauges keep their last reported value.
//
// Register once per service:
//
//	insp, _ := jobqueue.NewInspector(cfg)
//	prometheus.MustRegister(jobqueue.NewQueueMetricsCollector(insp, "solid-sync"))
//
// The "service" label disambiguates queues when a single Valkey hosts multiple
// services. Each service's /metrics endpoint exposes only its own queues
// (driven by Inspector.Queues()), so no extra filtering is needed in
// production — but the label is cheap and helps when scraping a shared queue.
type QueueMetricsCollector struct {
	inspector *asynq.Inspector
	service   string

	size        *prometheus.Desc
	pending     *prometheus.Desc
	active      *prometheus.Desc
	scheduled   *prometheus.Desc
	retry       *prometheus.Desc
	archived    *prometheus.Desc
	completed   *prometheus.Desc
	aggregating *prometheus.Desc
	latency     *prometheus.Desc
	memoryUsage *prometheus.Desc
	paused      *prometheus.Desc
	processed   *prometheus.Desc
	failed      *prometheus.Desc

	mu     sync.Mutex
	errors prometheus.Counter
}

// NewQueueMetricsCollector returns a collector wrapping the given Inspector.
// `service` becomes the value of the `service` label on every emitted metric.
func NewQueueMetricsCollector(inspector *asynq.Inspector, service string) *QueueMetricsCollector {
	labels := []string{"service", "queue"}
	c := &QueueMetricsCollector{
		inspector:   inspector,
		service:     service,
		size:        prometheus.NewDesc("asynq_queue_size", "Total tasks in queue (pending+active+scheduled+retry+aggregating+archived)", labels, nil),
		pending:     prometheus.NewDesc("asynq_queue_pending", "Pending tasks", labels, nil),
		active:      prometheus.NewDesc("asynq_queue_active", "Active (in-flight) tasks", labels, nil),
		scheduled:   prometheus.NewDesc("asynq_queue_scheduled", "Scheduled (future) tasks", labels, nil),
		retry:       prometheus.NewDesc("asynq_queue_retry", "Tasks awaiting retry", labels, nil),
		archived:    prometheus.NewDesc("asynq_queue_archived", "Archived (dead-lettered) tasks", labels, nil),
		completed:   prometheus.NewDesc("asynq_queue_completed", "Stored completed tasks (with retention)", labels, nil),
		aggregating: prometheus.NewDesc("asynq_queue_aggregating", "Aggregating tasks", labels, nil),
		latency:     prometheus.NewDesc("asynq_queue_latency_seconds", "Age of oldest pending task", labels, nil),
		memoryUsage: prometheus.NewDesc("asynq_queue_memory_bytes", "Approximate Redis memory usage for queue", labels, nil),
		paused:      prometheus.NewDesc("asynq_queue_paused", "1 if queue is paused, else 0", labels, nil),
		processed:   prometheus.NewDesc("asynq_queue_processed_today", "Tasks processed today (counter resets daily)", labels, nil),
		failed:      prometheus.NewDesc("asynq_queue_failed_today", "Tasks failed today (counter resets daily)", labels, nil),
		errors: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "asynq_inspector_errors_total",
			Help:        "Total errors from asynq.Inspector during scrape",
			ConstLabels: prometheus.Labels{"service": service},
		}),
	}
	return c
}

// Describe implements prometheus.Collector.
func (c *QueueMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.size
	ch <- c.pending
	ch <- c.active
	ch <- c.scheduled
	ch <- c.retry
	ch <- c.archived
	ch <- c.completed
	ch <- c.aggregating
	ch <- c.latency
	ch <- c.memoryUsage
	ch <- c.paused
	ch <- c.processed
	ch <- c.failed
	c.errors.Describe(ch)
}

// Collect implements prometheus.Collector. Called on every /metrics scrape.
func (c *QueueMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	queues, err := c.inspector.Queues()
	if err != nil {
		c.errors.Inc()
		c.errors.Collect(ch)
		return
	}

	for _, q := range queues {
		info, err := c.inspector.GetQueueInfo(q)
		if err != nil {
			c.errors.Inc()
			continue
		}
		paused := 0.0
		if info.Paused {
			paused = 1.0
		}
		labels := []string{c.service, info.Queue}
		ch <- prometheus.MustNewConstMetric(c.size, prometheus.GaugeValue, float64(info.Size), labels...)
		ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, float64(info.Pending), labels...)
		ch <- prometheus.MustNewConstMetric(c.active, prometheus.GaugeValue, float64(info.Active), labels...)
		ch <- prometheus.MustNewConstMetric(c.scheduled, prometheus.GaugeValue, float64(info.Scheduled), labels...)
		ch <- prometheus.MustNewConstMetric(c.retry, prometheus.GaugeValue, float64(info.Retry), labels...)
		ch <- prometheus.MustNewConstMetric(c.archived, prometheus.GaugeValue, float64(info.Archived), labels...)
		ch <- prometheus.MustNewConstMetric(c.completed, prometheus.GaugeValue, float64(info.Completed), labels...)
		ch <- prometheus.MustNewConstMetric(c.aggregating, prometheus.GaugeValue, float64(info.Aggregating), labels...)
		ch <- prometheus.MustNewConstMetric(c.latency, prometheus.GaugeValue, info.Latency.Seconds(), labels...)
		ch <- prometheus.MustNewConstMetric(c.memoryUsage, prometheus.GaugeValue, float64(info.MemoryUsage), labels...)
		ch <- prometheus.MustNewConstMetric(c.paused, prometheus.GaugeValue, paused, labels...)
		ch <- prometheus.MustNewConstMetric(c.processed, prometheus.GaugeValue, float64(info.Processed), labels...)
		ch <- prometheus.MustNewConstMetric(c.failed, prometheus.GaugeValue, float64(info.Failed), labels...)
	}
	c.errors.Collect(ch)
}
