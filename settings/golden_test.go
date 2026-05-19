// golden_test.go — Cross-runtime smoke test for settings ciphertext.
//
// Reads internal/testdata/golden/settings.json and asserts that every
// committed token (anthropic_api_key, openai_api_key, openrouter_api_key)
// still decrypts to the recorded plaintext. Pins the contract that a
// refactor of the encryption package will not silently make existing
// tenant_settings rows unreadable.
//
// Counterpart in Python: omkit-python/tests/test_settings_interop.py.

package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/omurlabs/omkit-go/encryption"
)

type settingsGoldenCase struct {
	Name       string `json:"name"`
	KeyB64     string `json:"key_b64"`
	Token      string `json:"token"`
	Plaintext  string `json:"plaintext"`
	ProducedBy string `json:"produced_by"`
}

func TestGolden_SettingsCiphertextDecrypt(t *testing.T) {
	path := filepath.Join("..", "internal", "testdata", "golden", "settings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var cases []settingsGoldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("golden file has no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := encryption.Decrypt(c.Token, c.KeyB64)
			if err != nil {
				t.Fatalf("decrypt %s (produced_by=%s): %v", c.Name, c.ProducedBy, err)
			}
			if got != c.Plaintext {
				t.Fatalf("plaintext drift %s (produced_by=%s):\n got: %q\nwant: %q", c.Name, c.ProducedBy, got, c.Plaintext)
			}
		})
	}
}
