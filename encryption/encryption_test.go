// encryption_test.go — Round-trip + cross-SDK contract tests for AES-256-GCM
// settings encryption. Mirrors omkit-python/tests/test_encryption.py.

package encryption

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	const plain = "super-secret-value"
	tok, err := Encrypt(plain, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(tok, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plain)
	}
}

func TestDifferentKeysFail(t *testing.T) {
	k1, _ := GenerateKey()
	k2, _ := GenerateKey()
	tok, err := Encrypt("hello", k1)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := Decrypt(tok, k2); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken on key mismatch, got %v", err)
	}
}

func TestEncryptEmptyString(t *testing.T) {
	k, _ := GenerateKey()
	tok, err := Encrypt("", k)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(tok, k)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "" {
		t.Fatalf("empty round-trip mismatch: got %q", got)
	}
}

func TestTokenVersionPrefix(t *testing.T) {
	k, _ := GenerateKey()
	tok, _ := Encrypt("hello", k)
	raw, err := base64.URLEncoding.DecodeString(tok)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(raw[:2]) != "v1" {
		t.Fatalf("expected v1 prefix, got %q", raw[:2])
	}
}

func TestTamperDetection(t *testing.T) {
	k, _ := GenerateKey()
	tok, _ := Encrypt("hello", k)
	raw, _ := base64.URLEncoding.DecodeString(tok)
	raw[len(raw)-1] ^= 0x01 // flip last byte of tag
	tampered := base64.URLEncoding.EncodeToString(raw)
	if _, err := Decrypt(tampered, k); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken on tamper, got %v", err)
	}
}

func TestInvalidKeySize(t *testing.T) {
	short := base64.URLEncoding.EncodeToString([]byte("too-short"))
	if _, err := Encrypt("x", short); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
	if _, err := Decrypt(short, short); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey on decrypt, got %v", err)
	}
}

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sk-abcdefghij1234", "sk-a****1234"},
		{"abcdefgh", "ab****gh"},
		{"abc", "****"},
		{"a", "****"},
		{"", ""},
		{"abcd", "ab****cd"},
		{"abcdefghij", "abcd****ghij"},
	}
	for _, c := range cases {
		if got := MaskSecret(c.in); got != c.want {
			t.Errorf("MaskSecret(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
