// Package sessions defines a pluggable session store interface used by services
// that need to persist short-lived per-user sessions. Backends include
// PostgresSessionStore (default) and RedisSessionStore (opt-in via env).
package sessions

import (
	"context"
	"time"
)

// Session is the canonical session record shared by all Store implementations.
type Session struct {
	Token     string
	TenantID  string
	Payload   []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store is the backend-agnostic contract for persisting sessions.
type Store interface {
	Get(ctx context.Context, token string) (*Session, error)
	Put(ctx context.Context, s *Session) error
	Delete(ctx context.Context, token string) error
	List(ctx context.Context, tenantID string) ([]*Session, error)
	Close() error
}
