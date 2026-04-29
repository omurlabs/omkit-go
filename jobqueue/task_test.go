package jobqueue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

const testTenant = "11111111-2222-3333-4444-555555555555"

func TestNewTaskWrapsPayloadInEnvelope(t *testing.T) {
	type fhirPush struct {
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
	}
	payload := fhirPush{ResourceType: "medication", ResourceID: "abc"}

	task, opts, err := NewTask("solid-sync:fhir-push", testTenant, payload)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if task.Type() != "solid-sync:fhir-push" {
		t.Errorf("task type = %q, want solid-sync:fhir-push", task.Type())
	}

	env, err := Unwrap(task.Payload())
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if env.TenantID != testTenant {
		t.Errorf("tenant_id = %q, want %q", env.TenantID, testTenant)
	}
	var inner fhirPush
	if err := json.Unmarshal(env.Payload, &inner); err != nil {
		t.Fatalf("inner unmarshal: %v", err)
	}
	if inner != payload {
		t.Errorf("inner payload = %+v, want %+v", inner, payload)
	}

	// Defaults: MaxRetry, Timeout, Retention. Asynq doesn't expose option
	// inspection — assert by count + that user opts append after.
	if len(opts) != 3 {
		t.Errorf("default option count = %d, want 3", len(opts))
	}
}

func TestNewTaskRejectsInvalidTenant(t *testing.T) {
	_, _, err := NewTask("t", "not-a-uuid", map[string]string{"a": "b"})
	if !IsInvalidEnvelopeError(err) {
		t.Fatalf("err = %v, want ErrInvalidEnvelope", err)
	}
}

func TestNewTaskUserOptionsAppendAfterDefaults(t *testing.T) {
	_, opts, err := NewTask("t", testTenant, struct{}{}, asynq.MaxRetry(99), asynq.Timeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	// 3 defaults + 2 user opts. User opts at the tail so Asynq processes them
	// last (later-wins).
	if len(opts) != 5 {
		t.Errorf("opt count = %d, want 5", len(opts))
	}
}
