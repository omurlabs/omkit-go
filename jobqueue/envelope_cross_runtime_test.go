// envelope_cross_runtime_test.go — Read omkit-python's golden envelopes.
//
// PR #7 pinned Python-reads-Go. This file closes the inverse direction
// (Go-reads-Python) so a refactor on either side fails CI before the bytes
// hit Valkey.
//
// Skips cleanly when the sibling omkit-python checkout is absent (local dev);
// CI runs paired and exercises the full set.
//
// Counterpart in Python: omkit-python/tests/test_cross_runtime_envelope.py.

package jobqueue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type pyEnvelopeCase struct {
	Name         string         `json:"name"`
	TenantID     string         `json:"tenant_id"`
	RequestID    string         `json:"request_id"`
	Payload      map[string]any `json:"payload"`
	ExpectedJSON string         `json:"expected_json"`
}

func loadPyEnvelopeGolden(t *testing.T) ([]pyEnvelopeCase, bool) {
	t.Helper()
	// Test cwd is the package dir (jobqueue/). Sibling omkit-python lives
	// two levels up at ../../omkit-python relative to here.
	path := filepath.Join("..", "..", "omkit-python", "tests", "golden", "envelope_v1.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read py golden: %v", err)
	}
	var cases []pyEnvelopeCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse py golden: %v", err)
	}
	return cases, true
}

func TestCrossRuntime_PyEnvelopeWrapMatchesBytes(t *testing.T) {
	cases, ok := loadPyEnvelopeGolden(t)
	if !ok {
		t.Skip("omkit-python sibling repo not present")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := WrapWithRequestID(c.TenantID, c.RequestID, c.Payload)
			if err != nil {
				t.Fatalf("wrap: %v", err)
			}
			if string(got) != c.ExpectedJSON {
				t.Fatalf("byte drift vs py-produced envelope:\n got: %s\nwant: %s", got, c.ExpectedJSON)
			}
		})
	}
}

func TestCrossRuntime_PyEnvelopeUnwrapParsesBytes(t *testing.T) {
	cases, ok := loadPyEnvelopeGolden(t)
	if !ok {
		t.Skip("omkit-python sibling repo not present")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			env, err := Unwrap([]byte(c.ExpectedJSON))
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
			var got map[string]any
			if err := json.Unmarshal(env.Payload, &got); err != nil {
				t.Fatalf("payload unmarshal: %v", err)
			}
			if len(got) != len(c.Payload) {
				t.Errorf("payload len: got %d want %d", len(got), len(c.Payload))
			}
			for k, v := range c.Payload {
				if got[k] != v {
					t.Errorf("payload[%q]: got %v want %v", k, got[k], v)
				}
			}
		})
	}
}
