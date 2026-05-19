// cross_runtime_test.go — Read omkit-python's golden encryption tokens.
//
// interop_test.go pins Go-produced tokens. This file closes the inverse
// direction (Decrypt Py-produced tokens) so a Python-side AAD / prefix /
// base64 drift fails Go CI immediately.
//
// Skips cleanly when the sibling omkit-python checkout is absent (local
// dev). CI runs paired and exercises the full set.
//
// Counterpart in Python: omkit-python/tests/test_cross_runtime_encryption.py.

package encryption

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type pyEncryptionCase struct {
	Name      string `json:"name"`
	KeyB64    string `json:"key_b64"`
	Plaintext string `json:"plaintext"`
	Token     string `json:"token"`
}

func loadPyEncryptionGolden(t *testing.T) ([]pyEncryptionCase, bool) {
	t.Helper()
	// Test cwd is the package dir (encryption/). Sibling omkit-python
	// lives two levels up at ../../omkit-python.
	path := filepath.Join("..", "..", "omkit-python", "tests", "golden", "encryption_v1.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read py golden: %v", err)
	}
	var cases []pyEncryptionCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse py golden: %v", err)
	}
	return cases, true
}

func TestCrossRuntime_DecryptPyProducedToken(t *testing.T) {
	cases, ok := loadPyEncryptionGolden(t)
	if !ok {
		t.Skip("omkit-python sibling repo not present")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := Decrypt(c.Token, c.KeyB64)
			if err != nil {
				t.Fatalf("decrypt: %v", err)
			}
			if got != c.Plaintext {
				t.Fatalf("plaintext drift from py-produced token:\n got: %q\nwant: %q", got, c.Plaintext)
			}
		})
	}
}
