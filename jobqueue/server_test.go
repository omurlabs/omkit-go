package jobqueue

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tenant"
)

func TestWithTenantInjectsTenantAndStripsEnvelope(t *testing.T) {
	body, err := Wrap(testTenant, map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	task := asynq.NewTask("solid-sync:fhir-push", body)

	var seenTenant string
	var seenInner []byte
	h := WithTenant(asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		seenTenant = tenant.FromContext(ctx)
		seenInner = t.Payload()
		return nil
	}))

	if err := h.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if seenTenant != testTenant {
		t.Errorf("tenant in ctx = %q, want %q", seenTenant, testTenant)
	}
	// Inner payload should be the raw object, not the envelope.
	if string(seenInner) != `{"k":"v"}` {
		t.Errorf("inner payload = %s, want raw object", seenInner)
	}
}

func TestWithTenantInvalidEnvelopeReturnsSkipRetry(t *testing.T) {
	task := asynq.NewTask("t", []byte(`{"version":1,"tenant_id":"not-a-uuid","payload":{}}`))

	called := false
	h := WithTenant(asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		called = true
		return nil
	}))

	err := h.ProcessTask(context.Background(), task)
	if err == nil {
		t.Fatal("ProcessTask returned nil error, want SkipRetry")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("err = %v, want errors.Is asynq.SkipRetry", err)
	}
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Errorf("err = %v, want errors.Is ErrInvalidEnvelope", err)
	}
	if called {
		t.Error("downstream handler invoked despite invalid envelope")
	}
}

func TestWithTenantFuncWrapsHandlerFunc(t *testing.T) {
	body, _ := Wrap(testTenant, map[string]int{"x": 1})
	task := asynq.NewTask("t", body)

	got := ""
	h := WithTenantFunc(func(ctx context.Context, _ *asynq.Task) error {
		got = tenant.FromContext(ctx)
		return nil
	})
	if err := h(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if got != testTenant {
		t.Errorf("tenant = %q, want %q", got, testTenant)
	}
}
