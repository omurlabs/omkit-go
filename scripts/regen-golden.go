//go:build ignore

// regen-golden.go — regenerate cross-runtime golden fixtures.
//
// Usage: go run scripts/regen-golden.go
//
// Writes:
//   - internal/testdata/golden/envelope.json
//   - internal/testdata/golden/encryption.json
//   - internal/testdata/golden/settings.json
//
// Envelope cases are deterministic (Go json.Marshal + sorted map keys).
// Encryption/settings tokens carry random nonces, so each run rotates them.
// Both SDKs read the committed values; regen + commit is the source of truth.

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/omurlabs/omkit-go/encryption"
	"github.com/omurlabs/omkit-go/jobqueue"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
	tenantC = "33333333-3333-3333-3333-333333333333"
)

type envelopeCase struct {
	Name        string          `json:"name"`
	TenantID    string          `json:"tenant_id"`
	RequestID   string          `json:"request_id"`
	Payload     json.RawMessage `json:"payload"`
	EnvelopeB64 string          `json:"envelope_bytes_b64"`
}

type secretCase struct {
	Name       string `json:"name"`
	KeyB64     string `json:"key_b64"`
	Token      string `json:"token"`
	Plaintext  string `json:"plaintext"`
	ProducedBy string `json:"produced_by"`
}

func main() {
	root, err := repoRoot()
	if err != nil {
		log.Fatalf("repo root: %v", err)
	}
	outDir := filepath.Join(root, "internal", "testdata", "golden")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	writeJSON(filepath.Join(outDir, "envelope.json"), buildEnvelopeCases())
	writeJSON(filepath.Join(outDir, "encryption.json"), buildEncryptionCases())
	writeJSON(filepath.Join(outDir, "settings.json"), buildSettingsCases())

	fmt.Printf("wrote fixtures to %s\n", outDir)
}

func buildEnvelopeCases() []envelopeCase {
	// Payloads use single-key or alphabetically-ordered keys so Go's sorted
	// json.Marshal output matches Python's insertion-ordered dump on the
	// other side of the contract.
	cases := []struct {
		name      string
		tenant    string
		requestID string
		payload   any
	}{
		{
			name:    "minimal-no-request-id",
			tenant:  tenantA,
			payload: map[string]any{"task": "ping"},
		},
		{
			name:      "with-request-id",
			tenant:    tenantB,
			requestID: "req-abcdef0123456789",
			payload:   map[string]any{"task": "noop"},
		},
		{
			name:      "sorted-multi-key-payload",
			tenant:    tenantC,
			requestID: "01HX5Z7N5GZ7QY1Q2Q3Q4Q5Q6Q",
			// Keys already alphabetical; Go sorts and Py preserves -> identical bytes.
			payload: map[string]any{"a": 1, "b": "two", "c": true},
		},
	}

	out := make([]envelopeCase, 0, len(cases))
	for _, c := range cases {
		bytes, err := jobqueue.WrapWithRequestID(c.tenant, c.requestID, c.payload)
		if err != nil {
			log.Fatalf("wrap %s: %v", c.name, err)
		}
		// Extract the marshalled payload bytes from the envelope so the
		// fixture records the exact opaque payload both sides will see.
		var env struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(bytes, &env); err != nil {
			log.Fatalf("re-parse %s: %v", c.name, err)
		}
		out = append(out, envelopeCase{
			Name:        c.name,
			TenantID:    c.tenant,
			RequestID:   c.requestID,
			Payload:     env.Payload,
			EnvelopeB64: base64.StdEncoding.EncodeToString(bytes),
		})
	}
	return out
}

func buildEncryptionCases() []secretCase {
	keys := []string{
		// Deterministic test keys (NOT for production use).
		base64.URLEncoding.EncodeToString(repeat32(0x01)),
		base64.URLEncoding.EncodeToString(repeat32(0x02)),
		base64.URLEncoding.EncodeToString(repeat32(0x03)),
	}
	plain := []string{
		"sk-ant-test-0123456789abcdef",
		"short",
		"",
	}
	out := make([]secretCase, 0, len(keys))
	for i, k := range keys {
		tok, err := encryption.Encrypt(plain[i], k)
		if err != nil {
			log.Fatalf("encrypt: %v", err)
		}
		out = append(out, secretCase{
			Name:       fmt.Sprintf("case-%d", i),
			KeyB64:     k,
			Token:      tok,
			Plaintext:  plain[i],
			ProducedBy: "go",
		})
	}
	return out
}

func buildSettingsCases() []secretCase {
	// Realistic tenant_settings / account_keys shapes.
	type s struct {
		name      string
		plaintext string
	}
	entries := []s{
		{"anthropic_api_key", "sk-ant-api03-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"},
		{"openai_api_key", "sk-proj-YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY"},
		{"openrouter_api_key", "sk-or-v1-ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"},
	}
	out := make([]secretCase, 0, len(entries))
	for i, e := range entries {
		k := base64.URLEncoding.EncodeToString(repeat32(byte(0x10 + i)))
		tok, err := encryption.Encrypt(e.plaintext, k)
		if err != nil {
			log.Fatalf("encrypt: %v", err)
		}
		out = append(out, secretCase{
			Name:       e.name,
			KeyB64:     k,
			Token:      tok,
			Plaintext:  e.plaintext,
			ProducedBy: "go",
		})
	}
	return out
}

func repeat32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

func writeJSON(path string, v any) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatalf("marshal %s: %v", path, err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
	}
	return "", fmt.Errorf("go.mod not found above %s", wd)
}
