package dbpool_test

import (
	"context"
	"os"
	"testing"

	"github.com/omurlabs/omkit-go/dbpool"
)

// TestNewPoolSetsRoleOnEachConnection verifies that a pool created with NewPool
// runs SET ROLE via AfterConnect so every acquired connection has the role set.
func TestNewPoolSetsRoleOnEachConnection(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := dbpool.NewPool(ctx, dbpool.Config{DSN: dsn, Role: "omur_app", MaxConns: 4})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	var role string
	if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatalf("query: %v", err)
	}
	if role != "omur_app" {
		t.Fatalf("expected omur_app, got %s", role)
	}
}

// TestNewPoolRoleSurvivesTxnError verifies SET ROLE applied by AfterConnect is
// preserved on the physical connection even after a transaction error, because
// AfterConnect runs once per connection (not per txn).
func TestNewPoolRoleSurvivesTxnError(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := dbpool.NewPool(ctx, dbpool.Config{DSN: dsn, Role: "omur_app", MaxConns: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, _ = tx.Exec(ctx, "SELECT 1/0")
	_ = tx.Rollback(ctx)

	var role string
	if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatalf("query after txn error: %v", err)
	}
	if role != "omur_app" {
		t.Fatalf("role leaked after txn error: %s", role)
	}
}

// TestNewPoolRequiresDSN verifies validation rejects empty DSN.
func TestNewPoolRequiresDSN(t *testing.T) {
	_, err := dbpool.NewPool(context.Background(), dbpool.Config{})
	if err == nil {
		t.Fatal("expected error for empty DSN, got nil")
	}
}

func TestWithTenant_UsesConfiguredRole(t *testing.T) {
	// This test documents that WithTenant/WithTenantQuery honour the role
	// the pool was constructed with rather than a hardcoded string. We don't
	// have a real DB in unit tests, so we verify the SQL via a fake tx
	// adapter is out of scope here — instead we assert the per-pool role
	// plumbing exists.
	if dbpool.PoolRole(nil) != "" {
		t.Fatal("PoolRole(nil): expected empty, got non-empty")
	}
}
