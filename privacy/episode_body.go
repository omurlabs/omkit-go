// Package privacy implements the Omur ↔ gnokee privacy boundary primitives.
//
// EncryptedBody is the wire envelope defined by ADR-029 "Encrypted Episode
// Body for gnokee Ingest". Helpers EncryptEpisodeBody / DecryptEpisodeBody
// are the only sanctioned producers / consumers; callers do not construct
// envelopes by hand and do not pass freeform AAD.
//
// The crypto layer is AES-256-GCM via the existing crypto.Wrap / crypto.Unwrap
// primitives. The KMS layer is the kms.KMS interface (ADR-024) — production
// adapters are OpenBao Transit, AWS KMS, GCP KMS; in-tree LocalDevKMS for tests.
package privacy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/crypto"
	"github.com/omurlabs/omur-core/packages/omur-go-sdk/kms"
)

// SchemaV1 is the only supported envelope schema for this package release.
const SchemaV1 = "omur.encrypted_body.v1"

// AlgAES256GCM is the only supported AEAD for SchemaV1.
const AlgAES256GCM = "AES-256-GCM"

const aadPrefix = "omur:gnokee:episode:"

// purposeEpisodeBody is the fixed KMS purpose for episode-body DEK wrapping.
// It is bound into the KMS-internal AAD prefix per ADR-024 and pinned here so
// callers cannot accidentally mix it with other purpose strings.
const purposeEpisodeBody = "gnokee_episode_body"

// EncryptedBody is the wire envelope handed across the Omur ↔ gnokee boundary.
//
// Byte-shaped fields are stored as base64url strings to make JSON round-trips
// total. Wrapped DEK travels in the envelope alongside the ciphertext: useless
// to gnokee without KMS access, and storing it there avoids a Spine round-trip
// on every recall.
type EncryptedBody struct {
	Schema     string `json:"schema"`
	KeyID      string `json:"key_id"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	AAD        string `json:"aad"`
	Alg        string `json:"alg"`
	WrappedDEK string `json:"wrapped_dek"`
}

// ErrUnsupportedSchema is returned when an envelope advertises a schema or
// algorithm this SDK release cannot handle.
var ErrUnsupportedSchema = errors.New("privacy: unsupported envelope schema or algorithm")

// ErrAADMismatch is returned when the (episode_id, tenant_id, schema_label)
// tuple supplied to DecryptEpisodeBody does not match the envelope's AAD field.
// Surfacing this before invoking KMS / GCM gives a clear error path; the GCM
// authentication tag would otherwise fail with an opaque error anyway.
var ErrAADMismatch = errors.New("privacy: AAD mismatch — (episode_id, tenant_id, schema_label) does not match envelope")

// BuildAAD constructs the literal AAD string for an episode body per ADR-029.
//
// The format is omur:gnokee:episode:<episodeID>:<tenantID>:<schemaLabel>.
// All three parts must be non-empty and must not contain ':' — the colon is
// the field delimiter and embedding one would corrupt the binding.
func BuildAAD(episodeID, tenantID, schemaLabel string) (string, error) {
	if episodeID == "" || tenantID == "" || schemaLabel == "" {
		return "", errors.New("privacy: episodeID, tenantID, and schemaLabel are all required for AAD construction")
	}
	for _, part := range []string{episodeID, tenantID, schemaLabel} {
		if strings.ContainsRune(part, ':') {
			return "", errors.New("privacy: AAD parts must not contain ':' — would corrupt the delimiter")
		}
	}
	return aadPrefix + episodeID + ":" + tenantID + ":" + schemaLabel, nil
}

// EncryptEpisodeBody encrypts plaintext for handoff to gnokee.
//
// Generates a fresh AES-256 DEK, encrypts under AES-256-GCM with a CSPRNG
// nonce, wraps the DEK via the KMS adapter, and returns the envelope. The
// unwrapped DEK is zeroised before return.
func EncryptEpisodeBody(
	ctx context.Context,
	k kms.KMS,
	plaintext []byte,
	tenantID, episodeID, schemaLabel string,
) (EncryptedBody, error) {
	aad, err := BuildAAD(episodeID, tenantID, schemaLabel)
	if err != nil {
		return EncryptedBody{}, err
	}
	aadBytes := []byte(aad)

	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return EncryptedBody{}, fmt.Errorf("privacy: dek generation: %w", err)
	}
	defer zero(dek)

	// crypto.Wrap returns nonce||ciphertext. Split for the envelope so the
	// nonce is independently inspectable (operators / audit tooling care).
	blob, err := crypto.Wrap(dek, plaintext, aadBytes)
	if err != nil {
		return EncryptedBody{}, fmt.Errorf("privacy: aes-gcm wrap: %w", err)
	}
	if len(blob) < 12 {
		return EncryptedBody{}, errors.New("privacy: crypto.Wrap returned short blob")
	}
	nonce := blob[:12]
	ciphertext := blob[12:]

	wrapped, version, err := k.WrapDEK(ctx, tenantID, purposeEpisodeBody, dek, aadBytes)
	if err != nil {
		return EncryptedBody{}, fmt.Errorf("privacy: kms WrapDEK: %w", err)
	}

	return EncryptedBody{
		Schema:     SchemaV1,
		KeyID:      "omur:tenant:" + tenantID + ":k_user:" + version,
		Nonce:      b64e(nonce),
		Ciphertext: b64e(ciphertext),
		AAD:        aad,
		Alg:        AlgAES256GCM,
		WrappedDEK: b64e(wrapped),
	}, nil
}

// DecryptEpisodeBody reverses EncryptEpisodeBody.
//
// Validates schema and AAD before invoking KMS / GCM so failure modes are
// distinguishable. The unwrapped DEK is zeroised after use.
func DecryptEpisodeBody(
	ctx context.Context,
	k kms.KMS,
	envelope EncryptedBody,
	tenantID, episodeID, schemaLabel string,
) ([]byte, error) {
	if envelope.Schema != SchemaV1 {
		return nil, fmt.Errorf("%w: schema=%q", ErrUnsupportedSchema, envelope.Schema)
	}
	if envelope.Alg != AlgAES256GCM {
		return nil, fmt.Errorf("%w: alg=%q", ErrUnsupportedSchema, envelope.Alg)
	}

	expectedAAD, err := BuildAAD(episodeID, tenantID, schemaLabel)
	if err != nil {
		return nil, err
	}
	if envelope.AAD != expectedAAD {
		return nil, ErrAADMismatch
	}
	aadBytes := []byte(expectedAAD)

	wrapped, err := b64d(envelope.WrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("privacy: decode wrapped_dek: %w", err)
	}
	dek, err := k.UnwrapDEK(ctx, tenantID, purposeEpisodeBody, wrapped, aadBytes)
	if err != nil {
		return nil, fmt.Errorf("privacy: kms UnwrapDEK: %w", err)
	}
	defer zero(dek)

	nonce, err := b64d(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("privacy: decode nonce: %w", err)
	}
	ciphertext, err := b64d(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("privacy: decode ciphertext: %w", err)
	}

	blob := make([]byte, 0, len(nonce)+len(ciphertext))
	blob = append(blob, nonce...)
	blob = append(blob, ciphertext...)
	return crypto.Unwrap(dek, blob, aadBytes)
}

func b64e(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64d(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
