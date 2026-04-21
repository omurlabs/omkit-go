package kms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"

	cryptopkg "github.com/omurlabs/omur-core/packages/omur-go-sdk/crypto"
)

// LocalDevKMS is an in-process adapter for dev and integration tests.
// It derives a key per keyID from a single master secret via HKDF-lite (HMAC-based).
// NEVER use in production — it has no access-control surface.
type LocalDevKMS struct {
	master [32]byte
}

func NewLocalDevKMS(master []byte) *LocalDevKMS {
	var k LocalDevKMS
	copy(k.master[:], master)
	return &k
}

// key derives a deterministic subkey per keyID using HMAC-SHA256.
func (l *LocalDevKMS) key(keyID string) []byte {
	mac := hmac.New(sha256.New, l.master[:])
	mac.Write([]byte("omur-kms-derive-v1:" + keyID))
	return mac.Sum(nil) // 32 bytes
}

func (l *LocalDevKMS) Wrap(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	return cryptopkg.Wrap(l.key(keyID), plaintext, aad)
}

func (l *LocalDevKMS) Unwrap(ctx context.Context, keyID string, blob, aad []byte) ([]byte, error) {
	return cryptopkg.Unwrap(l.key(keyID), blob, aad)
}

// CurrentVersion returns a static "v1" token. Cloud adapters return the
// backend's own key-version identifier.
func (l *LocalDevKMS) CurrentVersion(ctx context.Context, keyID string) (string, error) {
	return "v1", nil
}
