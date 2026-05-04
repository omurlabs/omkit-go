// localdev.go — localdev module.
//
// exports: LocalDevKMS | NewLocalDevKMS | Wrap | Unwrap | CurrentVersion | WrapDEK | UnwrapDEK | DeleteUserKeys
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

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

// userKey derives a per-(userID, purpose) wrapping key using HMAC-SHA256.
// The derivation label matches the Vault Transit derived-key convention:
// "user-" + userID + "-" + purpose, so keys are isolated by both dimensions.
func (l *LocalDevKMS) userKey(userID, purpose string) []byte {
	mac := hmac.New(sha256.New, l.master[:])
	mac.Write([]byte("user-" + userID + "-" + purpose))
	return mac.Sum(nil) // 32 bytes
}

// bindAAD prefixes caller-supplied aad with "purpose|userID|" so that the
// GCM authentication tag covers the binding. Cross-user or cross-purpose
// unwrap attempts fail even when the ciphertext blob is otherwise intact.
func bindAAD(purpose, userID string, aad []byte) []byte {
	prefix := []byte(purpose + "|" + userID + "|")
	bound := make([]byte, len(prefix)+len(aad))
	copy(bound, prefix)
	copy(bound[len(prefix):], aad)
	return bound
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

// WrapDEK encrypts plainDEK under the per-(userID, purpose) derived key.
// The effective AAD is "purpose|userID|" + aad, binding the wrapped blob to
// exactly this (user, purpose) pair. Returned version is "localdev-v1";
// cloud adapters return the backend key-version identifier.
func (l *LocalDevKMS) WrapDEK(ctx context.Context, userID, purpose string, plainDEK, aad []byte) (wrapped []byte, version string, err error) {
	kek := l.userKey(userID, purpose)
	bound := bindAAD(purpose, userID, aad)
	wrapped, err = cryptopkg.Wrap(kek, plainDEK, bound)
	if err != nil {
		return nil, "", err
	}
	return wrapped, "localdev-v1", nil
}

// UnwrapDEK reverses WrapDEK. The same (userID, purpose, aad) triple must be
// supplied; any mismatch produces an AES-GCM authentication-tag failure.
func (l *LocalDevKMS) UnwrapDEK(ctx context.Context, userID, purpose string, wrapped, aad []byte) (plainDEK []byte, err error) {
	kek := l.userKey(userID, purpose)
	bound := bindAAD(purpose, userID, aad)
	return cryptopkg.Unwrap(kek, wrapped, bound)
}

// DeleteUserKeys is a no-op for LocalDevKMS because wrapping keys are derived
// on demand from the master secret and are never stored. Cryptographic shred is
// therefore not possible with this adapter — deleting the master would remove
// ALL users' keys, not a single user's. Production adapters (Vault Transit,
// AWS KMS, GCP KMS) MUST implement actual per-user key deletion so that
// right-to-erasure (GDPR Art. 17 / Stage D5) is enforced at the KMS level.
func (l *LocalDevKMS) DeleteUserKeys(ctx context.Context, userID string) error {
	return nil
}
