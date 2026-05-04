// postgres.go — postgres module.
//
// exports: ErrNotFound | NewPostgresStore | Get | GetForTenant | Put | Delete | DeleteForTenant | List | Close
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

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

// Contract with respect to Postgres RLS (sessions_tenant_isolation):
//
//   - Put and List always work because the tenant is known; both run
//     inside a transaction that sets app.tenant_id so the policy is
//     satisfied under any role.
//   - Get and Delete take the opaque token. If the pool has
//     SET ROLE omur_app applied, RLS filters the SELECT/DELETE to zero
//     rows. Build the session pool with NewPool (no SET ROLE; superuser
//     bypasses RLS) so token lookup crosses tenants.
//
// Defense-in-depth: callers that know the expected tenant can call
// GetForTenant/DeleteForTenant, which verify/scope in-Go so a stolen
// token can't be used against a different tenant's row.
type postgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wraps a pgxpool in a Store that persists sessions to the
// `sessions` table. The caller owns the pool's lifecycle; Close is a no-op.
func NewPostgresStore(pool *pgxpool.Pool) Store {
	return &postgresStore{pool: pool}
}

func (s *postgresStore) Get(ctx context.Context, token string) (*Session, error) {
	return s.get(ctx, token, "")
}

// GetForTenant is Get with an expected tenant check. If the stored row's
// tenant_id doesn't match tenantID, ErrNotFound is returned.
func (s *postgresStore) GetForTenant(ctx context.Context, token, tenantID string) (*Session, error) {
	return s.get(ctx, token, tenantID)
}

func (s *postgresStore) get(ctx context.Context, token, expectedTenant string) (*Session, error) {
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
	if expectedTenant != "" && sess.TenantID != expectedTenant {
		return nil, ErrNotFound
	}
	return &sess, nil
}

func (s *postgresStore) Put(ctx context.Context, sess *Session) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Satisfy sessions_tenant_isolation regardless of whether the pool
	// runs as omur_app or a BYPASSRLS superuser. set_config(..., true)
	// is transaction-local so it doesn't leak across checkouts.
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, sess.TenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sessions (token, tenant_id, payload, expires_at)
		VALUES ($1, $2::uuid, $3, $4)
		ON CONFLICT (token) DO UPDATE SET
			payload = EXCLUDED.payload,
			expires_at = EXCLUDED.expires_at`,
		sess.Token, sess.TenantID, sess.Payload, sess.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *postgresStore) Delete(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

// DeleteForTenant deletes a session only if it belongs to tenantID.
// Preferred over Delete when the caller knows the tenant because it works
// regardless of whether the pool has SET ROLE omur_app applied.
func (s *postgresStore) DeleteForTenant(ctx context.Context, token, tenantID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token = $1 AND tenant_id = $2::uuid`, token, tenantID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *postgresStore) List(ctx context.Context, tenantID string) ([]*Session, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// Close is a no-op because the pgxpool lifecycle is owned by the caller who
// constructed this store.
func (s *postgresStore) Close() error { return nil }
