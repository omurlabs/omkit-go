package jobqueue

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tenant"
)

const corrTenant = "22222222-2222-2222-2222-222222222222"

func TestRequestIDCtxRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := RequestIDFromContext(ctx); got != "" {
		t.Errorf("empty ctx should yield empty request id, got %q", got)
	}
	ctx = WithRequestID(ctx, "abc-123")
	if got := RequestIDFromContext(ctx); got != "abc-123" {
		t.Errorf("got %q, want abc-123", got)
	}
}

func TestWrapWithRequestIDIncludesRequestID(t *testing.T) {
	data, err := WrapWithRequestID(corrTenant, "trace-xyz", testPayload{DocID: "x"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	env, err := Unwrap(data)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if env.RequestID != "trace-xyz" {
		t.Errorf("request_id: got %q want trace-xyz", env.RequestID)
	}
}

func TestWrapBackCompatOmitsRequestIDField(t *testing.T) {
	// Plain Wrap (the legacy two-arg signature) must NOT emit request_id
	// on the wire — keeps the byte format identical for callers that
	// don't care about correlation, so existing consumers of v1 envelopes
	// don't suddenly see a new field.
	data, err := Wrap(corrTenant, testPayload{DocID: "x"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if strings.Contains(string(data), "request_id") {
		t.Errorf("plain Wrap leaked request_id into wire format: %s", data)
	}

	// Round-trip is fine — empty string is the zero value.
	env, err := Unwrap(data)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if env.RequestID != "" {
		t.Errorf("expected empty request id, got %q", env.RequestID)
	}
}

func TestUnwrapToleratesOlderEnvelopes(t *testing.T) {
	// Envelope produced by an older worker (pre-G6) carries no request_id
	// field at all. Must decode without error.
	older := []byte(`{"version":1,"tenant_id":"` + corrTenant + `","payload":{"x":1}}`)
	env, err := Unwrap(older)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if env.RequestID != "" {
		t.Errorf("older envelope should yield empty request id, got %q", env.RequestID)
	}
}

func TestWithEnvelopePropagatesBothTenantAndRequestID(t *testing.T) {
	body, err := WrapWithRequestID(corrTenant, "req-99", testPayload{DocID: "y"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	var seenTenant, seenReq string
	handler := asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		seenTenant = tenant.FromContext(ctx)
		seenReq = RequestIDFromContext(ctx)
		// Confirm the inner payload is the unwrapped one, not the
		// envelope — same contract as WithTenant.
		var p testPayload
		_ = json.Unmarshal(t.Payload(), &p)
		if p.DocID != "y" {
			return &assertionError{msg: "payload not unwrapped"}
		}
		return nil
	})

	wrapped := WithEnvelope(handler)
	if err := wrapped.ProcessTask(context.Background(), asynq.NewTask("t", body)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if seenTenant != corrTenant {
		t.Errorf("tenant: got %q want %q", seenTenant, corrTenant)
	}
	if seenReq != "req-99" {
		t.Errorf("request_id: got %q want req-99", seenReq)
	}
}

func TestWithEnvelopeOmitsRequestIDWhenAbsent(t *testing.T) {
	// Plain Wrap (no request id) → ctx must NOT carry a request id.
	body, err := Wrap(corrTenant, testPayload{DocID: "z"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	var seenReq string
	handler := asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		seenReq = RequestIDFromContext(ctx)
		return nil
	})
	if err := WithEnvelope(handler).ProcessTask(context.Background(), asynq.NewTask("t", body)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if seenReq != "" {
		t.Errorf("expected empty request id, got %q", seenReq)
	}
}

type assertionError struct{ msg string }

func (e *assertionError) Error() string { return e.msg }
