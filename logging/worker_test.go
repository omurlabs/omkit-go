// worker_test.go — tests for WorkerContext helper.
//
// exports: TestWorkerContext_AssignsRequestID | TestWorkerContext_BindsWorkerAttr | TestWorkerContext_FreshIDPerCall | TestWorkerContext_PreservesParentValues
// rules:   none
// agent:   logging-e2b | claude | 2026-05-04 | claude | per-tick worker ctx helper tests
// message:

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/requestid"
)

func captureWorker(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "test-svc"))
	t.Cleanup(func() { slog.SetDefault(prev) })
	fn()
	return buf.String()
}

func TestWorkerContext_AssignsRequestID(t *testing.T) {
	out := captureWorker(t, func() {
		ctx, log := WorkerContext(context.Background(), "test-worker")
		log.Info("tick")
		if id := requestid.FromContext(ctx); id == "" {
			t.Fatalf("ctx must carry a request_id; got empty")
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("output not JSON: %v\nraw=%q", err, out)
	}
	id, _ := payload["request_id"].(string)
	if id == "" {
		t.Fatalf("log line missing request_id: %v", payload)
	}
}

func TestWorkerContext_BindsWorkerAttr(t *testing.T) {
	out := captureWorker(t, func() {
		_, log := WorkerContext(context.Background(), "tenant-quota-scrape")
		log.Info("tick")
	})
	var payload map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &payload)
	if payload["worker"] != "tenant-quota-scrape" {
		t.Fatalf("expected worker=tenant-quota-scrape, got %v", payload["worker"])
	}
}

func TestWorkerContext_FreshIDPerCall(t *testing.T) {
	ctx1, _ := WorkerContext(context.Background(), "w")
	ctx2, _ := WorkerContext(context.Background(), "w")
	id1 := requestid.FromContext(ctx1)
	id2 := requestid.FromContext(ctx2)
	if id1 == "" || id2 == "" {
		t.Fatalf("ids must be non-empty; got %q %q", id1, id2)
	}
	if id1 == id2 {
		t.Fatalf("each call must mint a fresh id; both were %q", id1)
	}
}

func TestWorkerContext_PreservesParentValues(t *testing.T) {
	type key struct{}
	parent := context.WithValue(context.Background(), key{}, "carry-me")
	ctx, _ := WorkerContext(parent, "w")
	if got, _ := ctx.Value(key{}).(string); got != "carry-me" {
		t.Fatalf("parent ctx values lost; got %q", got)
	}
}
