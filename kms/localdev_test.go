package kms

import (
	"context"
	"crypto/rand"
	"testing"
)

func TestLocalDevKMSRoundTrip(t *testing.T) {
	ctx := context.Background()
	var master [32]byte
	rand.Read(master[:])
	k := NewLocalDevKMS(master[:])

	plain := []byte("secret-payload")
	aad := []byte("provider=google,sub=123")
	ct, err := k.Wrap(ctx, "keyid-1", plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := k.Unwrap(ctx, "keyid-1", ct, aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != string(plain) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestLocalDevKMSAADMismatch(t *testing.T) {
	ctx := context.Background()
	var master [32]byte
	rand.Read(master[:])
	k := NewLocalDevKMS(master[:])
	ct, _ := k.Wrap(ctx, "keyid-1", []byte("x"), []byte("aad-a"))
	if _, err := k.Unwrap(ctx, "keyid-1", ct, []byte("aad-b")); err == nil {
		t.Fatal("expected aad mismatch to fail")
	}
}

func TestLocalDevKMSCurrentVersion(t *testing.T) {
	ctx := context.Background()
	var master [32]byte
	rand.Read(master[:])
	k := NewLocalDevKMS(master[:])
	v, err := k.CurrentVersion(ctx, "keyid-1")
	if err != nil {
		t.Fatal(err)
	}
	if v != "v1" {
		t.Fatalf("CurrentVersion = %q, want %q", v, "v1")
	}
}

// TestLocalDevKMSSatisfiesKMS is a compile-time assertion that LocalDevKMS
// satisfies the KMS interface (including CurrentVersion).
func TestLocalDevKMSSatisfiesKMS(t *testing.T) {
	var _ KMS = (*LocalDevKMS)(nil)
}
