package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
)

// AuditEntry describes a single admin action to be written to admin_audit_log.
// Caller must populate Role + Action; the helper fills in actor/request fields
// from the http.Request.
//
// SECURITY — Diff handling (caller discipline, no automated enforcement):
//
//   - Diff is `null` for reads, and `{before: ..., after: ...}` for mutations.
//   - NEVER include decrypted secrets, raw API keys, encryption material,
//     PHI, plaintext credentials, or any sensitive value. Use masked forms
//     only (e.g. {"masked_key": "sk-***abc"} — matches the existing
//     api_keys handler conventions).
//   - Phase 1 has no callers; before any Phase 2 mutation handler invokes
//     this helper, the audit reviewer MUST confirm every Diff payload is
//     either redacted or known-safe. There is no type-level guard.
type AuditEntry struct {
	Role       Role
	Action     string // e.g. "system_key.put"
	TargetKind string // e.g. "provider"
	TargetID   string // e.g. "anthropic"
	Diff       any    // marshalled to JSONB; pass nil to omit
}

// WriteAuditEntry persists an audit row. Synchronous — call from the handler
// after the underlying mutation succeeds. Caller decides whether to fail the
// request on audit-write error; this helper just propagates the DB error.
//
// Source of fields:
//   - actor_uid    ← X-Authentik-Uid          (empty when service-token call)
//   - actor_email  ← X-Authentik-Email
//   - actor_groups ← X-Authentik-Groups       (raw, pipe-separated)
//   - request_id   ← OTel trace ID            (16-byte hex, or empty)
//   - ip           ← X-Forwarded-For first hop, falling back to RemoteAddr
//   - user_agent   ← User-Agent header
func WriteAuditEntry(ctx context.Context, pool *pgxpool.Pool, r *http.Request, e AuditEntry) error {
	if pool == nil {
		return fmt.Errorf("audit: nil pool")
	}
	if e.Action == "" {
		return fmt.Errorf("audit: action required")
	}
	if e.Role == "" {
		return fmt.Errorf("audit: role required")
	}

	actorUID := r.Header.Get("X-Authentik-Uid")
	if actorUID == "" {
		actorUID = "service" // service-token callers
	}

	var diffJSON []byte
	if e.Diff != nil {
		var err error
		diffJSON, err = json.Marshal(e.Diff)
		if err != nil {
			return fmt.Errorf("audit: marshal diff: %w", err)
		}
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO admin_audit_log
			(actor_uid, actor_email, actor_groups, role, action, target_kind, target_id, diff, request_id, ip, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		actorUID,
		nullableHeader(r, "X-Authentik-Email"),
		nullableHeader(r, "X-Authentik-Groups"),
		string(e.Role),
		e.Action,
		nullableString(e.TargetKind),
		nullableString(e.TargetID),
		diffJSON, // pgx encodes nil []byte as SQL NULL for JSONB columns
		nullableString(traceID(ctx)),
		nullableString(clientIP(r)),
		nullableHeader(r, "User-Agent"),
	)
	return err
}

func nullableHeader(r *http.Request, name string) any {
	v := r.Header.Get(name)
	if v == "" {
		return nil
	}
	return v
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func traceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First hop is the original client; the rest is the proxy chain.
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	// RemoteAddr is host:port; we want just the host. Best-effort, not parsed
	// as a strict IP — IPv6 addresses can carry brackets.
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i > 0 {
		addr = addr[:i]
	}
	return strings.Trim(addr, "[]")
}
