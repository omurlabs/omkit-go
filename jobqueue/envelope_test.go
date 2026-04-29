package jobqueue

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const validTenant = "11111111-1111-1111-1111-111111111111"

type testPayload struct {
	DocID string `json:"doc_id"`
}

func TestWrapRoundTrip(t *testing.T) {
	data, err := Wrap(validTenant, testPayload{DocID: "abc"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	env, err := Unwrap(data)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if env.Version != EnvelopeVersion {
		t.Errorf("version: got %d want %d", env.Version, EnvelopeVersion)
	}
	if env.TenantID != validTenant {
		t.Errorf("tenant_id: got %q want %q", env.TenantID, validTenant)
	}
	var p testPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if p.DocID != "abc" {
		t.Errorf("payload doc_id: got %q want abc", p.DocID)
	}
}

func TestWrapRejectsBadTenant(t *testing.T) {
	_, err := Wrap("not-a-uuid", testPayload{DocID: "abc"})
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestUnwrapRejectsBadJSON(t *testing.T) {
	_, err := Unwrap([]byte("{not json"))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestUnwrapRejectsBadTenantID(t *testing.T) {
	bogus := []byte(`{"version":1,"tenant_id":"nope","payload":{}}`)
	_, err := Unwrap(bogus)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
	if !strings.Contains(err.Error(), "uuid") {
		t.Errorf("expected uuid in error, got %v", err)
	}
}

func TestUnwrapRejectsMissingVersion(t *testing.T) {
	bogus := []byte(`{"tenant_id":"` + validTenant + `","payload":{"x":1}}`)
	_, err := Unwrap(bogus)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestUnwrapRejectsFutureVersion(t *testing.T) {
	bogus := []byte(`{"version":99,"tenant_id":"` + validTenant + `","payload":{"x":1}}`)
	_, err := Unwrap(bogus)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
	if !strings.Contains(err.Error(), "unsupported version") {
		t.Errorf("expected version error, got %v", err)
	}
}

func TestUnwrapRejectsEmptyPayload(t *testing.T) {
	bogus := []byte(`{"version":1,"tenant_id":"` + validTenant + `"}`)
	_, err := Unwrap(bogus)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}
