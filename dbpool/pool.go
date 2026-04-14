// Package dbpool provides a pgx connection pool with RLS tenant isolation.
package dbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a pgx connection pool from a DSN string.
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
