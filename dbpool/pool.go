// Package dbpool provides a pgx connection pool with RLS tenant isolation.
package dbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config configures a pool created via NewPool.
type Config struct {
	DSN      string
	Role     string // application role to SET on every new connection (e.g. "omur_app")
	MaxConns int32  // pool max connections; zero means pgx default
}

// NewPool returns a pgxpool that runs `SET ROLE <role>` on every new connection
// via AfterConnect. This guarantees the role is set even if the application
// forgets, and survives reconnects without needing PgBouncer's server_reset_query.
// Callers should prefer NewPool over the legacy New when they want the defence-in-depth
// role-setting behaviour introduced alongside the PgBouncer removal.
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("dbpool: DSN required")
	}
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("dbpool: parse config: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	}
	if cfg.Role != "" {
		role := cfg.Role
		pcfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
			return err
		}
	}
	return pgxpool.NewWithConfig(ctx, pcfg)
}

// New creates a pgx connection pool from a DSN string.
// Deprecated: prefer NewPool which accepts a Config and sets the application role
// on every new connection via AfterConnect. Retained for existing callers.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("dbpool: parse config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("dbpool: connect: %w", err)
	}
	return pool, nil
}

// WithTenant acquires a connection, sets the RLS tenant context, and runs fn.
// The connection is returned to the pool after fn completes.
func WithTenant(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbpool: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SET ROLE omur_app"); err != nil {
		return fmt.Errorf("dbpool: set role: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("dbpool: set tenant: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WithTenantQuery is like WithTenant but for read-only queries that don't need commit.
func WithTenantQuery(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbpool: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SET ROLE omur_app"); err != nil {
		return fmt.Errorf("dbpool: set role: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("dbpool: set tenant: %w", err)
	}

	return fn(tx)
}
