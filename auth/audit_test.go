package auth

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWriteAuditEntry_PersistsRow(t *testing.T) {
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

	req := httptest.NewRequest("POST", "/admin/system-keys/anthropic", nil)
	req.Header.Set("X-Auth-Request-User", "audit-test-uid")
	req.Header.Set("X-Auth-Request-Email", "audit@example.com")
	req.Header.Set("X-Auth-Request-Groups", "omur-admin|tenant-default")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("User-Agent", "go-test/1.0")

	entry := AuditEntry{
		Role:       RoleAdmin,
		Action:     "system_key.put",
		TargetKind: "provider",
		TargetID:   "anthropic",
		Diff:       map[string]any{"before": nil, "after": map[string]any{"masked_key": "sk-***abc"}},
	}
	if err := WriteAuditEntry(ctx, pool, req, entry); err != nil {
		t.Fatalf("WriteAuditEntry: %v", err)
	}

	var (
		actorUID, role, action, ip string
		hasDiff                    bool
	)
	if err := pool.QueryRow(ctx,
		`SELECT actor_uid, role, action, ip, diff IS NOT NULL
		 FROM admin_audit_log
		 WHERE actor_uid = $1 AND action = $2
		 ORDER BY ts DESC LIMIT 1`,
		"audit-test-uid", "system_key.put",
	).Scan(&actorUID, &role, &action, &ip, &hasDiff); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if actorUID != "audit-test-uid" || role != "admin" || action != "system_key.put" {
		t.Errorf("row: actor=%q role=%q action=%q", actorUID, role, action)
	}
	if ip != "203.0.113.7" {
		t.Errorf("ip: got %q, want 203.0.113.7", ip)
	}
	if !hasDiff {
		t.Error("diff: got NULL, want non-null jsonb")
	}

	// Cleanup — append-only via app role, but tests use the superuser DSN.
	_, _ = pool.Exec(ctx,
		`DELETE FROM admin_audit_log WHERE actor_uid = $1`, "audit-test-uid")
}
