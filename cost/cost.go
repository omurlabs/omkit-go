// Package cost emits provider cost telemetry as a single Prometheus
// counter. Track 3 of
// docs/superpowers/plans/2026-05-03-cloud-readiness-prep.md.
//
// Cortex tracks LLM tokens and dollars; the rest of the stack —
// marrow embedder/extractor, cerebellum reranker, auris STT/TTS —
// emits no comparable signal. This package gives every service the
// same shape so "Projected Cloud Spend" panels can compare providers
// before any cloud switch is flipped.
//
// The counter omur_cost_units_total carries a fixed low-cardinality
// label set so VictoriaMetrics scrape stays cheap:
//
//   - service       — emitting service name (cortex, synapse, …).
//   - provider      — backend identifier (local, voyage, openai, …).
//   - op            — operation name (embed, parse_pages, rerank,
//                     stt_seconds, tts_chars).
//   - tenant_bucket — coarse tenant grouping (system / trial / paid);
//                     never the raw tenant_id (cardinality).
//
// The units argument is the billable count for the operation
// (tokens, pages, audio_seconds, …). Dollar projection lives out of
// scope — a static price-table joins counter values in Grafana.
package cost

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// CostUnitsTotal counts billable units emitted by provider calls.
var CostUnitsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "omur_cost_units_total",
		Help: "Billable units emitted by a provider call (tokens, pages, seconds, chars).",
	},
	[]string{"service", "provider", "op", "tenant_bucket"},
)

// validBuckets is the firewall against high-cardinality tenant IDs
// leaking into the metric. Unknown values normalise to "trial" so an
// instrumentation typo cannot silently inflate the "paid" bucket.
var validBuckets = map[string]struct{}{
	"system": {},
	"trial":  {},
	"paid":   {},
}

// RecordCost increments omur_cost_units_total for one provider call.
//
// units <= 0 is a no-op — counters cannot decrease and a non-positive
// input is always a caller bug. Emission failures (e.g. registry-side
// panic) are swallowed; the caller is on a hot path and a metric blip
// must not break it.
func RecordCost(service, provider, op string, units float64, tenantBucket string) {
	if units <= 0 {
		return
	}
	bucket := tenantBucket
	if _, ok := validBuckets[bucket]; !ok {
		bucket = "trial"
	}
	defer func() { _ = recover() }()
	CostUnitsTotal.WithLabelValues(service, provider, op, bucket).Add(units)
}
