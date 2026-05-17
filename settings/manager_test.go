package settings_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omkit-go/settings"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "settings.json")

	m := settings.New(nil, settings.WithCache(cachePath))

	// Manually apply changes to populate cache
	m.ApplyChange("key1", "val1")
	m.ApplyChange("key2", "val2")

	// Write cache to disk
	if err := m.WriteCache(); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	// New manager reads from disk cache
	m2 := settings.New(nil, settings.WithCache(cachePath))
	if err := m2.LoadCache(); err != nil {
		t.Fatalf("LoadCache: %v", err)
	}

	if got := m2.Get("key1"); got != "val1" {
		t.Errorf("key1: got %q, want %q", got, "val1")
	}
	if got := m2.Get("key2"); got != "val2" {
		t.Errorf("key2: got %q, want %q", got, "val2")
	}
}

func TestGet(t *testing.T) {
	m := settings.New(nil)
	m.ApplyChange("exists", "hello")

	if got := m.Get("exists"); got != "hello" {
		t.Errorf("exists: got %q, want %q", got, "hello")
	}
	if got := m.Get("missing"); got != "" {
		t.Errorf("missing: got %q, want empty", got)
	}
}

func TestOnChange(t *testing.T) {
	m := settings.New(nil)

	var gotKey, gotVal string
	m.OnChange("mykey", func(key, value string) {
		gotKey = key
		gotVal = value
	})

	m.ApplyChange("mykey", "myval")

	if gotKey != "mykey" {
		t.Errorf("key: got %q, want %q", gotKey, "mykey")
	}
	if gotVal != "myval" {
		t.Errorf("val: got %q, want %q", gotVal, "myval")
	}
}

func TestCacheFilePermissions(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "settings.json")

	m := settings.New(nil, settings.WithCache(cachePath))
	m.ApplyChange("k", "v")

	if err := m.WriteCache(); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions: got %o, want 0600", perm)
	}
}

func TestManagerPollingPicksUpUpdates(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	// Clean any prior test rows.
	_, _ = pool.Exec(ctx, "DELETE FROM app_settings WHERE key = 'test_polling_key'")
	defer pool.Exec(ctx, "DELETE FROM app_settings WHERE key = 'test_polling_key'")

	t.Setenv("OMUR_SETTINGS_BACKEND", "postgres")
	mgr := settings.NewFromConfig(settings.Config{
		Pool:         pool,
		TenantID:     "00000000-0000-0000-0000-000000000099",
		PollInterval: 100 * time.Millisecond,
	})
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()

	_, err := pool.Exec(ctx, `
		INSERT INTO app_settings
			(key, value_json, value_type, category, label)
		VALUES
			('test_polling_key', '"polling_value"'::jsonb, 'string', 'test', 'polling test')
		ON CONFLICT (key) DO UPDATE SET
			value_json = EXCLUDED.value_json,
			updated_at = now()`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.Get("test_polling_key") == "polling_value" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected polling_value, got %q", mgr.Get("test_polling_key"))
}
