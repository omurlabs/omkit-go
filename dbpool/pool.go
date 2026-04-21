// Package dbpool provides a pgx connection pool with RLS tenant isolation.
package dbpool

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// poolRoles tracks the AfterConnect role for pools built via NewPool, so that
// WithTenant / WithTenantQuery use the same role name when setting up the
// transaction. Legacy New (no Role) returns "" via PoolRole and WithTenant
// falls back to the historical "omur_app" default.
var (
	poolRolesMu sync.RWMutex
	poolRoles   = map[*pgxpool.Pool]string{}
)

func rememberRole(p *pgxpool.Pool, role string) {
	if role == "" {
		return
	}
	poolRolesMu.Lock()
	poolRoles[p] = role
	poolRolesMu.Unlock()
}

// PoolRole returns the role configured for a pool via NewPool, or "" when the
// pool was built with the legacy New constructor (no role set).
func PoolRole(p *pgxpool.Pool) string {
	if p == nil {
		return ""
	}
	poolRolesMu.RLock()
	defer poolRolesMu.RUnlock()
	return poolRoles[p]
}

func roleForPool(p *pgxpool.Pool) string {
	if r := PoolRole(p); r != "" {
		return r
	}
	return "omur_app"
}

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
	// PgBouncer-compat: stay off server-side prepared statements so swapping
	// postgres:5432 for pgbouncer:6432 (transaction mode) remains a config-only
	// change. Extended query protocol + binary params still work; the only
	// trade-off is a few percent of single-query throughput at large scale.
	pcfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
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
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, err
	}
	rememberRole(pool, cfg.Role)
	return pool, nil
}

// New creates a pgx connection pool from a DSN string without SET ROLE.
//
// Deprecated: prefer NewPool with a Config.Role. Callers that keep using
// this constructor run as the connecting role (typically superuser), which
// BYPASSes RLS. The only supported use is cross-tenant aggregation where
// that bypass is intentional (e.g. services/pulse). Every other call site
// should migrate to NewPool(Role: "omur_app"). Retained for existing
// superuser-intentional callers; new code MUST NOT use it.
//
// Static analysis: this decl carries the canonical SA1019 `Deprecated:`
// prefix so staticcheck flags new usages.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("dbpool: parse config: %w", err)
	}
	// PgBouncer-compat: see NewPool for rationale.
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("dbpool: connect: %w", err)
	}
	return pool, nil
}

// NewSessionPool builds a pgxpool suitable for sessions.NewPostgresStore.
//
// The session pool intentionally does NOT run SET ROLE omur_app on each
// new connection. Token-based session lookup (Get/Delete) takes an opaque
// token without knowing the tenant, so SELECT/DELETE under a role subject
// to the sessions_tenant_isolation RLS policy would silently return zero
// rows. Connecting as the default omur superuser (which has BYPASSRLS)
// lets token lookup cross tenants; writes (Put, List) still run inside a
// transaction that sets app.tenant_id so RLS is honored for multi-tenant
// mutations.
//
// Use this helper when wiring SessionStore in a service's main. For pools
// that back RLS-enforced app queries, use NewPool with Role="omur_app".
func NewSessionPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return NewPool(ctx, Config{DSN: dsn})
}

// WithTenant acquires a connection, sets the RLS tenant context, and runs fn.
// The connection is returned to the pool after fn completes.
func WithTenant(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbpool: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	role := roleForPool(pool)
	if _, err := tx.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
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

// Superuser acquires a connection without setting a restrictive role, runs fn
// inside a transaction, and commits. The connection bypasses RLS because the
// pool user has BYPASSRLS (or is a superuser). Use this only for cross-tenant
// lookups where the tenant is not yet known (e.g. credential-handle → tenant_id
// at login time).
func Superuser(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbpool: begin: %w", err)
	}
	defer tx.Rollback(ctx)
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

	role := roleForPool(pool)
	if _, err := tx.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
		return fmt.Errorf("dbpool: set role: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("dbpool: set tenant: %w", err)
	}

	return fn(tx)
}
