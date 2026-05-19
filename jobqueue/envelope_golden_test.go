// envelope_golden_test.go — Cross-runtime golden tests for Envelope.
//
// Reads internal/testdata/golden/envelope.json and asserts:
//
//   1. Wrap reproduces the committed bytes exactly (sorted-key payloads only).
//   2. Unwrap parses the committed bytes back into matching Envelope fields.
//   3. EnvelopeVersion still equals 1 — bumping the constant without regenerating
//      fixtures and coordinating with omkit-python fails CI by design.
//
// Counterpart in Python: omkit-python/tests/test_jobqueue_envelope_interop.py
// reads the same JSON file.

package jobqueue

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type envelopeGoldenCase struct {
	Name        string          `json:"name"`
	TenantID    string          `json:"tenant_id"`
	RequestID   string          `json:"request_id"`
	Payload     json.RawMessage `json:"payload"`
	EnvelopeB64 string          `json:"envelope_bytes_b64"`
}

func loadEnvelopeGolden(t *testing.T) []envelopeGoldenCase {
	t.Helper()
	path := filepath.Join("..", "internal", "testdata", "golden", "envelope.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var cases []envelopeGoldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("golden file has no cases")
	}
	return cases
}

func TestGolden_EnvelopeVersionPin(t *testing.T) {
	// CI guard. If EnvelopeVersion bumps, regenerate fixtures
	// (scripts/regen-golden.go) and bump this constant + the Python pin
	// in the same release. Otherwise the SDKs silently diverge.
	if EnvelopeVersion != 1 {
		t.Fatalf("EnvelopeVersion changed to %d. Regenerate golden fixtures and coordinate with omkit-python before relaxing this assertion.", EnvelopeVersion)
	}
}

func TestGolden_EnvelopeWrapMatchesBytes(t *testing.T) {
	for _, c := range loadEnvelopeGolden(t) {
		t.Run(c.Name, func(t *testing.T) {
			var payload any
			if err := json.Unmarshal(c.Payload, &payload); err != nil {
				t.Fatalf("payload unmarshal: %v", err)
			}
			got, err := WrapWithRequestID(c.TenantID, c.RequestID, payload)
			if err != nil {
				t.Fatalf("wrap: %v", err)
			}
			want, err := base64.StdEncoding.DecodeString(c.EnvelopeB64)
			if err != nil {
				t.Fatalf("decode want: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("envelope bytes drift:\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestGolden_EnvelopeUnwrapParsesBytes(t *testing.T) {
	for _, c := range loadEnvelopeGolden(t) {
		t.Run(c.Name, func(t *testing.T) {
			raw, err := base64.StdEncoding.DecodeString(c.EnvelopeB64)
			if err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			env, err := Unwrap(raw)
			if err != nil {
				t.Fatalf("unwrap: %v", err)
			}
			if env.Version != EnvelopeVersion {
				t.Errorf("version: got %d want %d", env.Version, EnvelopeVersion)
			}
			if env.TenantID != c.TenantID {
				t.Errorf("tenant_id: got %q want %q", env.TenantID, c.TenantID)
			}
			if env.RequestID != c.RequestID {
				t.Errorf("request_id: got %q want %q", env.RequestID, c.RequestID)
			}
			// Fixture payload is pretty-printed (MarshalIndent re-formats
			// nested RawMessage). Compact before byte compare.
			var want bytes.Buffer
			if err := json.Compact(&want, c.Payload); err != nil {
				t.Fatalf("compact want payload: %v", err)
			}
			if !bytes.Equal(env.Payload, want.Bytes()) {
				t.Errorf("payload bytes:\n got: %s\nwant: %s", env.Payload, want.Bytes())
			}
		})
	}
}
