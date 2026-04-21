package kms

import "context"

// KMS is the interface Omur services call for ops-held wrapping.
// Production deployments inject an AWS/GCP/Vault-backed implementation.
type KMS interface {
	Wrap(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)
	Unwrap(ctx context.Context, keyID string, blob, aad []byte) ([]byte, error)
	// CurrentVersion returns an opaque version token (e.g. "v1") for keyID.
	// Callers use this for rotation bookkeeping; adapters may return the
	// underlying cloud key version (AWS KMS key-material id, GCP CryptoKeyVersion).
	CurrentVersion(ctx context.Context, keyID string) (string, error)
}
