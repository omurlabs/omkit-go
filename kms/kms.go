// kms.go — kms module.
//
// exports: KMS
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package kms

import "context"

// KMS is the interface Omur services call for ops-held wrapping.
// Production deployments inject an AWS/GCP/Vault-backed implementation.
//
// Static-key flow (tenant / system api_key encryption):
//   - Wrap / Unwrap operate on a named keyID owned by ops.
//
// DEK envelope flow (per-user document encryption, Stage C/D):
//   - WrapDEK / UnwrapDEK derive a per-(userID, purpose) wrapping key and
//     bind the operation to caller-supplied AAD. The AAD is prefixed with
//     "purpose|userID|" before encryption so cross-user and cross-purpose
//     unwrap attempts fail with an authentication error even if the caller
//     presents a seemingly valid blob.
//   - DeleteUserKeys supports cryptographic shred on right-to-erasure: once
//     the wrapping key material is deleted, all DEKs wrapped under it become
//     permanently inaccessible regardless of whether the ciphertext is retained.
type KMS interface {
	Wrap(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)
	Unwrap(ctx context.Context, keyID string, blob, aad []byte) ([]byte, error)
	// CurrentVersion returns an opaque version token (e.g. "v1") for keyID.
	// Callers use this for rotation bookkeeping; adapters may return the
	// underlying cloud key version (AWS KMS key-material id, GCP CryptoKeyVersion).
	CurrentVersion(ctx context.Context, keyID string) (string, error)

	// WrapDEK encrypts a plaintext DEK under a key derived for (userID, purpose).
	// aad is additional associated data supplied by the caller (e.g. document ID).
	// The implementation MUST prefix aad with "purpose|userID|" before using it
	// as GCM AAD so that cross-user and cross-purpose unwrap attempts are rejected
	// by the authentication tag even before any application-layer check.
	// version identifies the wrapping key version used; callers store it alongside
	// the wrapped blob to support future key rotation.
	WrapDEK(ctx context.Context, userID, purpose string, plainDEK, aad []byte) (wrapped []byte, version string, err error)

	// UnwrapDEK reverses WrapDEK. The same (userID, purpose, aad) triple must be
	// supplied; any mismatch causes an authentication-tag failure.
	UnwrapDEK(ctx context.Context, userID, purpose string, wrapped, aad []byte) (plainDEK []byte, err error)

	// DeleteUserKeys irrevocably destroys all wrapping key material for userID,
	// rendering every DEK previously wrapped under that user's keys permanently
	// inaccessible. This is the cryptographic shred primitive for right-to-erasure
	// (GDPR Art. 17 / Stage D5). Implementations MUST delete the key material from
	// the backing store; a no-op implementation defeats the purpose of this call.
	DeleteUserKeys(ctx context.Context, userID string) error
}
