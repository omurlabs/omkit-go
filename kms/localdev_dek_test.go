package kms

import (
	"bytes"
	"context"
	"crypto/rand"
	"strings"
	"testing"
)

func newTestKMS(t *testing.T) *LocalDevKMS {
	t.Helper()
	var master [32]byte
	if _, err := rand.Read(master[:]); err != nil {
		t.Fatal(err)
	}
	return NewLocalDevKMS(master[:])
}

func TestWrapDEKRoundTrip(t *testing.T) {
	ctx := context.Background()
	k := newTestKMS(t)

	plainDEK := make([]byte, 32)
	rand.Read(plainDEK)
	aad := []byte("doc-id=abc123")

	wrapped, version, err := k.WrapDEK(ctx, "user-1", "K_content", plainDEK, aad)
	if err != nil {
		t.Fatalf("WrapDEK: %v", err)
	}
	if version != "localdev-v1" {
		t.Fatalf("version = %q, want localdev-v1", version)
	}

	got, err := k.UnwrapDEK(ctx, "user-1", "K_content", wrapped, aad)
	if err != nil {
		t.Fatalf("UnwrapDEK: %v", err)
	}
	if !bytes.Equal(got, plainDEK) {
		t.Fatal("round-trip mismatch: recovered DEK differs from original")
	}
}

func TestWrapDEKAADTamper(t *testing.T) {
	ctx := context.Background()
	k := newTestKMS(t)

	plainDEK := make([]byte, 32)
	rand.Read(plainDEK)
	aad := []byte("doc-id=abc123")

	wrapped, _, err := k.WrapDEK(ctx, "user-1", "K_content", plainDEK, aad)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate a byte in the caller-supplied AAD.
	tampered := append([]byte(nil), aad...)
	tampered[0] ^= 0xFF

	_, err = k.UnwrapDEK(ctx, "user-1", "K_content", wrapped, tampered)
	if err == nil {
		t.Fatal("expected auth failure when AAD is tampered, got nil error")
	}
}

func TestWrapDEKCrossUserIsolation(t *testing.T) {
	ctx := context.Background()
	k := newTestKMS(t)

	plainDEK := make([]byte, 32)
	rand.Read(plainDEK)
	aad := []byte("doc-id=abc123")

	wrapped, _, err := k.WrapDEK(ctx, "user-A", "K_content", plainDEK, aad)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to unwrap using a different user ID.
	_, err = k.UnwrapDEK(ctx, "user-B", "K_content", wrapped, aad)
	if err == nil {
		t.Fatal("cross-user unwrap succeeded; expected auth failure")
	}
}

func TestWrapDEKCrossPurposeIsolation(t *testing.T) {
	ctx := context.Background()
	k := newTestKMS(t)

	plainDEK := make([]byte, 32)
	rand.Read(plainDEK)
	aad := []byte("doc-id=abc123")

	wrapped, _, err := k.WrapDEK(ctx, "user-1", "K_content", plainDEK, aad)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to unwrap under a different purpose.
	_, err = k.UnwrapDEK(ctx, "user-1", "K_meta", wrapped, aad)
	if err == nil {
		t.Fatal("cross-purpose unwrap succeeded; expected auth failure")
	}
}

func TestWrapDEKEmptyDEK(t *testing.T) {
	ctx := context.Background()
	k := newTestKMS(t)

	// AES-GCM can encrypt zero-length plaintext; round-trip must still work.
	wrapped, _, err := k.WrapDEK(ctx, "user-1", "K_content", []byte{}, []byte("aad"))
	if err != nil {
		t.Fatalf("WrapDEK empty DEK: %v", err)
	}
	got, err := k.UnwrapDEK(ctx, "user-1", "K_content", wrapped, []byte("aad"))
	if err != nil {
		t.Fatalf("UnwrapDEK empty DEK: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(got))
	}
}

func TestWrapDEKOversizedDEK(t *testing.T) {
	ctx := context.Background()
	k := newTestKMS(t)

	// 4 KiB DEK (atypical but must not panic or fail on size alone).
	large := make([]byte, 4096)
	rand.Read(large)
	aad := []byte("aad")

	wrapped, _, err := k.WrapDEK(ctx, "user-1", "K_content", large, aad)
	if err != nil {
		t.Fatalf("WrapDEK large DEK: %v", err)
	}
	got, err := k.UnwrapDEK(ctx, "user-1", "K_content", wrapped, aad)
	if err != nil {
		t.Fatalf("UnwrapDEK large DEK: %v", err)
	}
	if !bytes.Equal(got, large) {
		t.Fatal("large DEK round-trip mismatch")
	}
}

func TestDeleteUserKeysNoError(t *testing.T) {
	ctx := context.Background()
	k := newTestKMS(t)
	if err := k.DeleteUserKeys(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteUserKeys: %v", err)
	}
}

// TestLocalDevKMSSatisfiesExtendedKMS is a compile-time assertion that
// LocalDevKMS satisfies the full KMS interface including the new DEK methods.
func TestLocalDevKMSSatisfiesExtendedKMS(t *testing.T) {
	var _ KMS = (*LocalDevKMS)(nil)
}

// TestBindAADPrefixContent verifies that bindAAD produces the expected prefix
// so callers can reason about the AAD layout independently of AES-GCM.
func TestBindAADPrefixContent(t *testing.T) {
	result := bindAAD("K_content", "user-1", []byte("extra"))
	s := string(result)
	if !strings.HasPrefix(s, "K_content|user-1|") {
		t.Fatalf("bindAAD prefix unexpected: %q", s)
	}
	if !strings.HasSuffix(s, "extra") {
		t.Fatalf("bindAAD suffix unexpected: %q", s)
	}
}
