// events.go — events module.
//
// exports: SecurityEvent | WriteSecurityEvent
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package security

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SecurityEvent describes a single RAG security observation to be written
// to the security_events table.
//
// Evidence must contain structured metadata only (pattern names, classifier
// verdicts, stripped URLs). Never include full document content.
//
// Kind taxonomy:
//   - "sanitiser_pattern_hit"   — input sanitiser matched a blocked pattern
//   - "classifier_malicious"    — injection classifier returned malicious verdict
//   - "output_filter_strip"     — output filter removed a URL or unsafe element
//   - "doc_quarantined"         — document quarantined during ingest
//   - "citation_invalid"        — citation validation failed at retrieval time
//   - "rls_assert_failed"       — tenant RLS assertion mismatch detected
//
// Severity values: "info" | "warn" | "block"
type SecurityEvent struct {
	TenantID          string
	Kind              string
	Severity          string
	DocID             *string // nil when not document-scoped
	ChunkID           *string // nil when not retrieval-scoped
	Evidence          any     // marshalled to jsonb; pass nil for empty object
	ClassifierVersion *string
	RequestID         *string
}

// WriteSecurityEvent persists a security_events row. The caller is responsible
// for ensuring the connection's app.tenant_id GUC matches SecurityEvent.TenantID
// before calling — this helper does not set the GUC itself so it can be used
// in both standalone inserts and within caller-managed transactions.
//
// Returns an error if pool is nil, TenantID is empty, Kind is empty, or
// Severity is empty.
func WriteSecurityEvent(ctx context.Context, pool *pgxpool.Pool, e SecurityEvent) error {
	if pool == nil {
		return fmt.Errorf("security: nil pool")
	}
	if e.TenantID == "" {
		return fmt.Errorf("security: tenant_id required")
	}
	if e.Kind == "" {
		return fmt.Errorf("security: kind required")
	}
	if e.Severity == "" {
		return fmt.Errorf("security: severity required")
	}

	// Marshal evidence to a string so it round-trips through simple-protocol
	// mode (PgBouncer compat) without being mis-encoded as bytea. See the
	// identical rationale in packages/omur-go-sdk/auth/audit.go.
	var evidenceParam any
	if e.Evidence != nil {
		b, err := json.Marshal(e.Evidence)
		if err != nil {
			return fmt.Errorf("security: marshal evidence: %w", err)
		}
		evidenceParam = string(b)
	} else {
		evidenceParam = "{}"
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO security_events
			(tenant_id, kind, severity, doc_id, chunk_id,
			 evidence, classifier_version, request_id)
		 VALUES ($1::uuid, $2, $3, $4::uuid, $5, $6::jsonb, $7, $8)`,
		e.TenantID,
		e.Kind,
		e.Severity,
		e.DocID,
		e.ChunkID,
		evidenceParam,
		e.ClassifierVersion,
		e.RequestID,
	)
	return err
}
