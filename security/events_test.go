package security

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWriteSecurityEvent_NilPool(t *testing.T) {
	err := WriteSecurityEvent(context.Background(), nil, SecurityEvent{
		TenantID: "00000000-0000-0000-0000-000000000001",
		Kind:     "sanitiser_pattern_hit",
		Severity: "warn",
	})
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
}

func TestWriteSecurityEvent_MissingTenantID(t *testing.T) {
	pool := &pgxpool.Pool{} // non-nil but unusable — validation fires first
	err := WriteSecurityEvent(context.Background(), pool, SecurityEvent{
		Kind:     "sanitiser_pattern_hit",
		Severity: "warn",
	})
	if err == nil {
		t.Fatal("expected error for missing tenant_id, got nil")
	}
}

func TestWriteSecurityEvent_MissingKind(t *testing.T) {
	pool := &pgxpool.Pool{}
	err := WriteSecurityEvent(context.Background(), pool, SecurityEvent{
		TenantID: "00000000-0000-0000-0000-000000000001",
		Severity: "warn",
	})
	if err == nil {
		t.Fatal("expected error for missing kind, got nil")
	}
}

func TestWriteSecurityEvent_MissingSeverity(t *testing.T) {
	pool := &pgxpool.Pool{}
	err := WriteSecurityEvent(context.Background(), pool, SecurityEvent{
		TenantID: "00000000-0000-0000-0000-000000000001",
		Kind:     "sanitiser_pattern_hit",
	})
	if err == nil {
		t.Fatal("expected error for missing severity, got nil")
	}
}

// TestWriteSecurityEvent_PersistsRow and TestWriteSecurityEvent_RLSIsolation
// require a live Postgres instance with the migration applied.
func TestWriteSecurityEvent_PersistsRow(t *testing.T) {
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

	tenantID := "00000000-0000-0000-0000-000000000099"
	reqID := "test-req-" + t.Name()

	// Set GUC so RLS allows the insert.
	_, err = pool.Exec(ctx, "SET app.tenant_id = '"+tenantID+"'")
	if err != nil {
		t.Fatalf("set GUC: %v", err)
	}

	ev := SecurityEvent{
		TenantID:  tenantID,
		Kind:      "sanitiser_pattern_hit",
		Severity:  "warn",
		Evidence:  map[string]any{"pattern": "test_pattern"},
		RequestID: &reqID,
	}
	if err := WriteSecurityEvent(ctx, pool, ev); err != nil {
		t.Fatalf("WriteSecurityEvent: %v", err)
	}

	var kind, severity string
	if err := pool.QueryRow(ctx,
		`SELECT kind, severity FROM security_events
		 WHERE request_id = $1 ORDER BY occurred_at DESC LIMIT 1`,
		reqID,
	).Scan(&kind, &severity); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if kind != "sanitiser_pattern_hit" || severity != "warn" {
		t.Errorf("row: kind=%q severity=%q", kind, severity)
	}

	// Cleanup
	_, _ = pool.Exec(ctx, `DELETE FROM security_events WHERE request_id = $1`, reqID)
}

// TestWriteSecurityEvent_NoGUCReturnsError verifies that inserting without
// the app.tenant_id GUC set causes the RLS policy to reject the row when
// the pool connects as a role subject to RLS (omur_app).
func TestWriteSecurityEvent_NoGUCReturnsError(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL_APP_ROLE")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL_APP_ROLE not set — skipping RLS rejection test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	tenantID := "00000000-0000-0000-0000-000000000099"
	ev := SecurityEvent{
		TenantID: tenantID,
		Kind:     "rls_assert_failed",
		Severity: "block",
	}
	err = WriteSecurityEvent(ctx, pool, ev)
	if err == nil {
		t.Fatal("expected error when GUC not set on RLS-restricted role, got nil")
	}
}
