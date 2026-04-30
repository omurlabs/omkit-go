// envelope.go — envelope module.
//
// exports: EnvelopeVersion | Envelope | ErrInvalidEnvelope | Wrap | Unwrap
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package jobqueue provides shared primitives for Asynq-backed job queues.
//
// The Envelope is the cross-SDK contract: every task payload enqueued from any
// Omur service is wrapped in {version, tenant_id, payload}. Workers unwrap on
// receive, validate, and run the handler under the tenant's RLS scope.
package jobqueue

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// EnvelopeVersion is the current envelope schema version. Increment only on
// breaking shape changes; additive fields stay at v1 and are tolerated by
// older workers via json.RawMessage decoding.
const EnvelopeVersion = 1

// Envelope wraps every task payload with the tenant identifier and a schema
// version. Payload is opaque JSON — handlers unmarshal into their own type.
type Envelope struct {
	Version  int             `json:"version"`
	TenantID string          `json:"tenant_id"`
	Payload  json.RawMessage `json:"payload"`
}

// ErrInvalidEnvelope is returned when an inbound envelope fails validation.
// Workers should dead-letter (no retry) on this error — retrying a malformed
// envelope cannot succeed.
var ErrInvalidEnvelope = errors.New("invalid job envelope")

// Wrap marshals payload into an Envelope. tenant_id must be a valid UUID.
func Wrap(tenantID string, payload any) ([]byte, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("%w: tenant_id not a valid uuid: %v", ErrInvalidEnvelope, err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	env := Envelope{
		Version:  EnvelopeVersion,
		TenantID: tenantID,
		Payload:  raw,
	}
	return json.Marshal(env)
}

// Unwrap parses raw task data into an Envelope and validates required fields.
// Returns ErrInvalidEnvelope (wrapped) for any validation failure — callers
// should dead-letter rather than retry.
func Unwrap(data []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if env.Version == 0 {
		return nil, fmt.Errorf("%w: missing version", ErrInvalidEnvelope)
	}
	if env.Version > EnvelopeVersion {
		return nil, fmt.Errorf("%w: unsupported version %d (max %d)", ErrInvalidEnvelope, env.Version, EnvelopeVersion)
	}
	if _, err := uuid.Parse(env.TenantID); err != nil {
		return nil, fmt.Errorf("%w: tenant_id not a valid uuid: %v", ErrInvalidEnvelope, err)
	}
	if len(env.Payload) == 0 {
		return nil, fmt.Errorf("%w: empty payload", ErrInvalidEnvelope)
	}
	return &env, nil
}
