package security

// Integration tests for migration 017: omur_admin role gates feature_flags writes.
//
// Required env vars:
//
//	TEST_DATABASE_URL          — superuser DSN (e.g. postgres://omur:…@localhost/omur)
//	TEST_DATABASE_URL_APP_ROLE — same DB, connection that will SET ROLE omur_app
//
// These tests are skipped when the env vars are unset, so they do not block
// unit-only CI runs.

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMig017_AppRoleCannotWriteFeatureFlags verifies that after migration 017
// the omur_app role receives "permission denied" for INSERT and UPDATE on
// feature_flags, but SELECT still works.
func TestMig017_AppRoleCannotWriteFeatureFlags(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL_APP_ROLE")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL_APP_ROLE not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	// Explicitly switch to omur_app role so the test is meaningful even when
	// the DSN connects as a superuser.
	if _, err := pool.Exec(ctx, "SET ROLE omur_app"); err != nil {
		t.Fatalf("SET ROLE omur_app: %v", err)
	}

	// INSERT must fail.
	_, insertErr := pool.Exec(ctx,
		`INSERT INTO feature_flags (flag_name, enabled) VALUES ('mig017_probe', false)`)
	if insertErr == nil {
		_, _ = pool.Exec(ctx, `DELETE FROM feature_flags WHERE flag_name = 'mig017_probe'`)
		t.Fatal("INSERT succeeded as omur_app — REVOKE in migration 017 is not in effect")
	}
	t.Logf("INSERT denied (expected): %v", insertErr)

	// UPDATE must fail.
	_, updateErr := pool.Exec(ctx,
		`UPDATE feature_flags SET enabled = true WHERE flag_name = 'byok'`)
	if updateErr == nil {
		t.Fatal("UPDATE succeeded as omur_app — REVOKE in migration 017 is not in effect")
	}
	t.Logf("UPDATE denied (expected): %v", updateErr)

	// SELECT must succeed — read path must remain functional.
	var n int
	if scanErr := pool.QueryRow(ctx, `SELECT COUNT(*) FROM feature_flags`).Scan(&n); scanErr != nil {
		t.Fatalf("SELECT as omur_app failed: %v", scanErr)
	}
	t.Logf("SELECT OK — %d rows visible", n)
}

// TestMig017_AdminRoleCanWriteFeatureFlags verifies that omur_admin (the role
// created by migration 017) can INSERT, UPDATE, and DELETE on feature_flags.
// The write is wrapped in a transaction that is rolled back so no test data
// persists.
func TestMig017_AdminRoleCanWriteFeatureFlags(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	// Verify the role exists before trying to switch to it.
	var roleExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = 'omur_admin')`).Scan(&roleExists); err != nil {
		t.Fatalf("pg_roles query: %v", err)
	}
	if !roleExists {
		t.Skip("omur_admin role not found — migration 017 not applied")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) // cleans up test data unconditionally

	// SET LOCAL ROLE mirrors dbpool.WithPrivilegedRole usage in production.
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE omur_admin"); err != nil {
		t.Fatalf("SET LOCAL ROLE omur_admin: %v", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO feature_flags (flag_name, enabled)
		 VALUES ('mig017_admin_probe', false)
		 ON CONFLICT (tenant_id, flag_name) DO NOTHING`); err != nil {
		t.Fatalf("INSERT as omur_admin: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE feature_flags SET enabled = true WHERE flag_name = 'mig017_admin_probe'`); err != nil {
		t.Fatalf("UPDATE as omur_admin: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM feature_flags WHERE flag_name = 'mig017_admin_probe'`); err != nil {
		t.Fatalf("DELETE as omur_admin: %v", err)
	}
	t.Log("omur_admin INSERT/UPDATE/DELETE on feature_flags: all permitted")
	// Rollback automatically reverts the probe row.
}

// TestMig017_SecurityEventWrittenOnFlagMutation verifies the security_events
// row pattern used by the admin handler: inside a WithTenant transaction,
// app.tenant_id is set via set_config and the INSERT succeeds.
func TestMig017_SecurityEventWrittenOnFlagMutation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	const systemTenantID = "00000000-0000-0000-0000-000000000000"
	const testRequestID = "mig017-sec-event-test"

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	// Mirror the dbpool.WithTenant GUC setup.
	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.tenant_id', $1, true)`, systemTenantID); err != nil {
		t.Fatalf("set_config app.tenant_id: %v", err)
	}

	// Insert a feature_flag.set security event — same SQL the admin handler emits.
	if _, err := tx.Exec(ctx,
		`INSERT INTO security_events (tenant_id, kind, severity, evidence, request_id)
		 VALUES ($1::uuid, 'feature_flag.set', 'warn',
		         '{"flag":"flag.cloud_llm_proxy","value":{"enabled":true}}'::jsonb, $2)`,
		systemTenantID, testRequestID,
	); err != nil {
		t.Fatalf("INSERT security_events: %v", err)
	}

	// Read it back within the same transaction to confirm the row is present.
	var kind string
	if err := tx.QueryRow(ctx,
		`SELECT kind FROM security_events WHERE request_id = $1`, testRequestID,
	).Scan(&kind); err != nil {
		t.Fatalf("read security event: %v", err)
	}
	if kind != "feature_flag.set" {
		t.Errorf("kind: got %q, want %q", kind, "feature_flag.set")
	}
	t.Log("security_events row for feature_flag.set confirmed inside tx")
	// Rollback removes the test row.
}
