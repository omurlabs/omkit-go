package tenant_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestSourceColumnPreventsCrossIdPCollision asserts that a Zitadel user
// whose snowflake happens to match a legacy Authentik auth_uid cannot
// resolve to the Authentik tenant, and vice versa.
//
// Uses the (source, auth_uid) lookup predicate the SDK will issue after
// PR-3. Goes green once PR-2's migration + composite PK are in place.
func TestSourceColumnPreventsCrossIdPCollision(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Isolate from any row other tests may have left behind.
	const colliding = "198261369861120001"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM user_auth_map WHERE auth_uid = $1`, colliding)
		_, _ = db.ExecContext(ctx, `DELETE FROM tenants WHERE name IN ('collision-test-authentik', 'collision-test-zitadel')`)
	})

	// Seed two distinct tenants for the test — FK requires parent rows.
	var authTenantID, zitadelTenantID string
	mustExec(t, db, `INSERT INTO tenants (id, name) VALUES (gen_random_uuid(), 'collision-test-authentik') RETURNING id`).Scan(&authTenantID)
	mustExec(t, db, `INSERT INTO tenants (id, name) VALUES (gen_random_uuid(), 'collision-test-zitadel') RETURNING id`).Scan(&zitadelTenantID)

	// Two rows sharing auth_uid but differing in source — must both insert.
	mustExec(t, db, `INSERT INTO user_auth_map (auth_uid, tenant_id, source) VALUES ($1, $2, 'authentik')`, colliding, authTenantID)
	mustExec(t, db, `INSERT INTO user_auth_map (auth_uid, tenant_id, source) VALUES ($1, $2, 'zitadel')`, colliding, zitadelTenantID)

	if got := queryTenant(t, db, "authentik", colliding); got != authTenantID {
		t.Fatalf("authentik resolution: got %q, want %q", got, authTenantID)
	}
	if got := queryTenant(t, db, "zitadel", colliding); got != zitadelTenantID {
		t.Fatalf("zitadel resolution: got %q, want %q", got, zitadelTenantID)
	}
}

func queryTenant(t *testing.T, db *sql.DB, source, authUID string) string {
	t.Helper()
	var tid string
	err := db.QueryRow(
		`SELECT tenant_id::text FROM user_auth_map WHERE source = $1 AND auth_uid = $2`,
		source, authUID,
	).Scan(&tid)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return tid
}

// mustExec runs a statement and returns the row for RETURNING queries.
func mustExec(t *testing.T, db *sql.DB, q string, args ...any) *sql.Row {
	t.Helper()
	return db.QueryRowContext(context.Background(), q, args...)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("OMUR_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/omur?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("postgres not reachable at %s: %v", dsn, err)
	}
	return db
}
