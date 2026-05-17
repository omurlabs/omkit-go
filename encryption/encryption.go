// encryption.go — AES-256-GCM string encryption for tenant settings secrets.
//
// Thin string-in / string-out wrapper around `github.com/omurlabs/omkit-go/crypto`.
// Used by settings stores (tenant_settings, account_keys, system_keys) that hold
// a short, latency-insensitive secret as a URL-safe base64 token.
//
// Wire format (versioned):
//
//	base64.urlsafe("v1" || nonce(12) || ciphertext || tag(16))
//
// "v1" is a hard prefix so future rotations can ship a "v2" next to it without
// guesswork. AAD is fixed to []byte("omkit.encryption.v1"); cross-module ciphertext
// swaps fail at the GCM auth tag rather than silently decrypting.
//
// Cross-SDK contract: byte-identical to omkit-python's `omkit.encryption`.
//
// exports: ErrInvalidToken | ErrInvalidKey | KeySize | GenerateKey | Encrypt | Decrypt | MaskSecret
// rules:   API surface (GenerateKey, Encrypt, Decrypt, MaskSecret) is stable. Internal crypto MUST come from omkit-go/crypto — no second AEAD impl in this module.
// agent:   claude-opus-4-7 | anthropic | 2026-05-17 | claude-code | replaced Fernet/AES-CBC+HMAC with AES-256-GCM

// Package encryption provides AES-256-GCM encryption for tenant-scoped settings secrets.
package encryption

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/omurlabs/omkit-go/crypto"
)

// KeySize is the required raw-byte length of an `Encrypt`/`Decrypt` key.
const KeySize = 32

var (
	// ErrInvalidToken is returned when the ciphertext is malformed, the
	// version prefix mismatches, or the GCM authentication tag fails.
	ErrInvalidToken = errors.New("invalid or corrupted token")
	// ErrInvalidKey is returned when the key cannot be decoded to KeySize
	// bytes of URL-safe base64.
	ErrInvalidKey = errors.New("invalid key: must be 32 bytes URL-safe base64")

	versionPrefix = []byte("v1")
	aad           = []byte("omkit.encryption.v1")
)

// GenerateKey creates a fresh URL-safe base64 32-byte key.
//
// Output decodes back to exactly 32 raw bytes via decodeKey. Store in a secret
// manager; never log the value.
func GenerateKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(key), nil
}

// Encrypt encrypts plaintext under key. Returns a URL-safe base64 token whose
// payload is `"v1" || nonce || ciphertext || tag`. Empty plaintext is supported.
func Encrypt(plaintext, key string) (string, error) {
	kek, err := decodeKey(key)
	if err != nil {
		return "", err
	}
	blob, err := crypto.Wrap(kek, []byte(plaintext), aad)
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, len(versionPrefix)+len(blob))
	out = append(out, versionPrefix...)
	out = append(out, blob...)
	return base64.URLEncoding.EncodeToString(out), nil
}

// Decrypt reverses Encrypt. Returns ErrInvalidToken on version mismatch, base64
// failure, or AEAD tag failure (wrong key, mutated bytes, AAD drift).
func Decrypt(token, key string) (string, error) {
	kek, err := decodeKey(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return "", ErrInvalidToken
	}
	if len(raw) < len(versionPrefix)+12+16 {
		return "", ErrInvalidToken
	}
	for i, b := range versionPrefix {
		if raw[i] != b {
			return "", ErrInvalidToken
		}
	}
	blob := raw[len(versionPrefix):]
	plain, err := crypto.Unwrap(kek, blob, aad)
	if err != nil {
		return "", ErrInvalidToken
	}
	return string(plain), nil
}

// MaskSecret masks a secret for display.
//
//   - n >= 10: first 4 + "****" + last 4
//   - 4 <= n <  10: first 2 + "****" + last 2
//   - n  < 4: "****"
//   - n == 0: ""
func MaskSecret(value string) string {
	n := len(value)
	if n == 0 {
		return ""
	}
	if n >= 10 {
		return value[:4] + "****" + value[n-4:]
	}
	if n >= 4 {
		return value[:2] + "****" + value[n-2:]
	}
	return "****"
}

func decodeKey(key string) ([]byte, error) {
	k, err := base64.URLEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	if len(k) != KeySize {
		return nil, ErrInvalidKey
	}
	return k, nil
}
