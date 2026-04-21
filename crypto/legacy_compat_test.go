package crypto

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// TestLegacyBlobsDecrypt pins that ciphertext blobs produced by the pre-move
// services/spine/auth.Wrap (and equivalently by the SDK crypto.Wrap, which
// TestGoldenVectorUnwrap proves is byte-for-byte compatible) remain decryptable
// via the current SDK crypto.Unwrap. If this test fails, some production
// ciphertext may have become undecryptable and the move is not safe.
func TestLegacyBlobsDecrypt(t *testing.T) {
	data, err := os.ReadFile("testdata/legacy_blobs.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		KEKHex    string `json:"kek_hex"`
		AAD       string `json:"aad"`
		Plaintext string `json:"plaintext"`
		BlobHex   string `json:"blob_hex"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 10 {
		t.Fatalf("expected >=10 legacy cases, got %d", len(cases))
	}
	for i, c := range cases {
		kek, err := hex.DecodeString(c.KEKHex)
		if err != nil {
			t.Fatalf("case %d: bad kek hex: %v", i, err)
		}
		blob, err := hex.DecodeString(c.BlobHex)
		if err != nil {
			t.Fatalf("case %d: bad blob hex: %v", i, err)
		}
		out, err := Unwrap(kek, blob, []byte(c.AAD))
		if err != nil {
			t.Fatalf("case %d (aad=%q): unwrap failed: %v", i, c.AAD, err)
		}
		if string(out) != c.Plaintext {
			t.Fatalf("case %d: plaintext drift: got %q want %q", i, out, c.Plaintext)
		}
	}
}
