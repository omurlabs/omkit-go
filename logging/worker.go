// worker.go — WorkerContext helper for pure-bg-worker request_id propagation.
//
// exports: WorkerContext
// rules:   per-tick UUID, never per-worker-run; worker is a regular slog attr (NOT a VL stream field)
// agent:   logging-e2b | claude | 2026-05-04 | claude | helper for bg worker per-tick ctx
// message: minted UUID + worker attr per call so each tick correlates its own log lines

package logging

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/requestid"
)

// WorkerContext returns a child of parent with a fresh UUID v4 attached as the
// request_id, plus a logger pre-bound with `worker=<name>`. Use it at the top
// of each iteration of a pure background worker so every log line emitted on
// that tick carries a unique correlation id and a stable worker tag — both
// joinable in VictoriaLogs.
//
// `name` is a stable identifier for the worker class (e.g. "tenant-quota-scrape",
// "model-pull", "settings-reload-listener"). It must NOT include per-tick or
// per-replica state — that's what the per-tick request_id is for. Treat name
// as a regular slog attribute, never a VictoriaLogs stream field, since high
// cardinality on stream fields blows up VL's index.
//
// Calling WorkerContext per-tick (not per-worker-lifetime) is the contract:
// each tick of a poll loop is its own logical "request" for correlation
// purposes. Per-run ids would bury thousands of ticks under one id and erase
// the join.
func WorkerContext(parent context.Context, name string) (context.Context, *slog.Logger) {
	if parent == nil {
		parent = context.Background()
	}
	ctx := requestid.NewContext(parent, uuid.NewString())
	return ctx, FromContext(ctx).With("worker", name)
}
