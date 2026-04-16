package sessions

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by Get when no matching session is found or the
// session has expired.
var ErrNotFound = errors.New("sessions: not found")

type postgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wraps a pgxpool in a Store that persists sessions to the
// `sessions` table. The caller owns the pool's lifecycle; Close is a no-op.
func NewPostgresStore(pool *pgxpool.Pool) Store {
	return &postgresStore{pool: pool}
}

func (s *postgresStore) Get(ctx context.Context, token string) (*Session, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT token, tenant_id::text, payload, created_at, expires_at
		FROM sessions
		WHERE token = $1 AND expires_at > now()`, token)
	var sess Session
	if err := row.Scan(&sess.Token, &sess.TenantID, &sess.Payload, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sess, nil
}

func (s *postgresStore) Put(ctx context.Context, sess *Session) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token, tenant_id, payload, expires_at)
		VALUES ($1, $2::uuid, $3, $4)
		ON CONFLICT (token) DO UPDATE SET
			payload = EXCLUDED.payload,
			expires_at = EXCLUDED.expires_at`,
		sess.Token, sess.TenantID, sess.Payload, sess.ExpiresAt)
	return err
}

func (s *postgresStore) Delete(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

func (s *postgresStore) List(ctx context.Context, tenantID string) ([]*Session, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT token, tenant_id::text, payload, created_at, expires_at
		FROM sessions
		WHERE tenant_id = $1::uuid AND expires_at > now()
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.Token, &sess.TenantID, &sess.Payload, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}

// Close is a no-op because the pgxpool lifecycle is owned by the caller who
// constructed this store.
func (s *postgresStore) Close() error { return nil }
