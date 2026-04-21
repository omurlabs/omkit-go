package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestWrapRoundTrip(t *testing.T) {
	var kek [32]byte
	rand.Read(kek[:])
	var secret [32]byte
	rand.Read(secret[:])

	blob, err := Wrap(kek[:], secret[:], []byte("omur-wrap-v1"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unwrap(kek[:], blob, []byte("omur-wrap-v1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(secret[:]) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestWrapAADMismatchFails(t *testing.T) {
	var kek [32]byte
	rand.Read(kek[:])
	blob, _ := Wrap(kek[:], []byte("hello"), []byte("aad-a"))
	if _, err := Unwrap(kek[:], blob, []byte("aad-b")); err == nil {
		t.Fatal("expected aad mismatch to fail")
	}
}

// TestGoldenVectorUnwrap pins byte-for-byte parity against a ciphertext blob
// produced by the pre-move services/spine/auth.Wrap. Proves the SDK move did
// not alter the AES-256-GCM envelope format.
func TestGoldenVectorUnwrap(t *testing.T) {
	kek, err := hex.DecodeString("0101010101010101010101010101010101010101010101010101010101010101")
	if err != nil {
		t.Fatal(err)
	}
	blob, err := hex.DecodeString("8d81c09a2a470251c953cb6e19b2c6fc5f4cd7579eda3e42cc627e7462135abd7d3aa4f8892fe47d6f35b0ccc8383edf8ed6afd1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unwrap(kek, blob, []byte("omur-wrap-v1"))
	if err != nil {
		t.Fatalf("golden unwrap failed: %v", err)
	}
	const want = "omur-golden-plaintext-v1"
	if string(out) != want {
		t.Fatalf("golden plaintext mismatch: got %q want %q", out, want)
	}
}
