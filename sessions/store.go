// store.go — store module.
//
// exports: Session | Store
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package sessions defines a pluggable session store interface used by services
// that need to persist short-lived per-user sessions. Backends include
// PostgresSessionStore (default) and RedisSessionStore (opt-in via env).
package sessions

import (
	"context"
	"encoding/json"
	"time"
)

// Session is the canonical session record shared by all Store implementations.
type Session struct {
	Token     string          `json:"token"`
	TenantID  string          `json:"tenant_id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// Store is the backend-agnostic contract for persisting sessions.
type Store interface {
	Get(ctx context.Context, token string) (*Session, error)
	Put(ctx context.Context, s *Session) error
	Delete(ctx context.Context, token string) error
	List(ctx context.Context, tenantID string) ([]*Session, error)
	Close() error
}
