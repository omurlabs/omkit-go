package sessions_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omkit-go/sessions"
)

func TestStoreInterfaceShape(t *testing.T) {
	var _ sessions.Store = (*fakeStore)(nil)
}

type fakeStore struct{}

func (f *fakeStore) Get(ctx context.Context, token string) (*sessions.Session, error) {
	return nil, nil
}
func (f *fakeStore) Put(ctx context.Context, s *sessions.Session) error { return nil }
func (f *fakeStore) Delete(ctx context.Context, token string) error    { return nil }
func (f *fakeStore) List(ctx context.Context, tenantID string) ([]*sessions.Session, error) {
	return nil, nil
}
func (f *fakeStore) Close() error { return nil }

func TestSessionFields(t *testing.T) {
	s := &sessions.Session{
		Token:     "tok",
		TenantID:  "ten",
		Payload:   []byte(`{"k":"v"}`),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if s.Token != "tok" {
		t.Fatal("token field")
	}
}

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

func TestPostgresStorePutGetDelete(t *testing.T) {
	pool := newTestPool(t)
	store := sessions.NewPostgresStore(pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			tenant_id UUID NOT NULL,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			expires_at TIMESTAMPTZ NOT NULL
		)`)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM sessions WHERE token LIKE 'test-%'")

	s := &sessions.Session{
		Token:     "test-1",
		TenantID:  "00000000-0000-0000-0000-000000000001",
		Payload:   []byte(`{"hello":"world"}`),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Put(ctx, s); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("payload parse: %v (raw=%s)", err, got.Payload)
	}
	if payload["hello"] != "world" {
		t.Fatalf("payload roundtrip failed: %s", got.Payload)
	}
	if err := store.Delete(ctx, "test-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "test-1")
	if err != sessions.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresStoreExpired(t *testing.T) {
	pool := newTestPool(t)
	store := sessions.NewPostgresStore(pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			tenant_id UUID NOT NULL,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			expires_at TIMESTAMPTZ NOT NULL
		)`)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	s := &sessions.Session{
		Token:     "test-expired",
		TenantID:  "00000000-0000-0000-0000-000000000001",
		Payload:   []byte(`{}`),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	_ = store.Put(ctx, s)
	defer pool.Exec(ctx, "DELETE FROM sessions WHERE token = 'test-expired'")

	_, err = store.Get(ctx, "test-expired")
	if err != sessions.ErrNotFound {
		t.Fatalf("expected expired session to return ErrNotFound, got %v", err)
	}
}

func TestPostgresStoreList(t *testing.T) {
	pool := newTestPool(t)
	store := sessions.NewPostgresStore(pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			tenant_id UUID NOT NULL,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			expires_at TIMESTAMPTZ NOT NULL
		)`)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM sessions WHERE token LIKE 'list-test-%'")

	tenant := "00000000-0000-0000-0000-000000000002"
	for i, tok := range []string{"list-test-a", "list-test-b"} {
		_ = i
		if err := store.Put(ctx, &sessions.Session{
			Token:     tok,
			TenantID:  tenant,
			Payload:   []byte(`{}`),
			ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("put %s: %v", tok, err)
		}
	}
	got, err := store.List(ctx, tenant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}
}

func TestRedisStorePutGetDelete(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	store, err := sessions.NewRedisStore(sessions.RedisConfig{
		Addr:      addr,
		Password:  os.Getenv("TEST_REDIS_PASSWORD"),
		KeyPrefix: "test:omur:session:",
	})
	if err != nil {
		t.Fatalf("NewRedisStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	s := &sessions.Session{
		Token:     "rtest-1",
		TenantID:  "00000000-0000-0000-0000-000000000001",
		Payload:   []byte(`{"k":1}`),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Put(ctx, s); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(ctx, "rtest-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Payload) != `{"k":1}` {
		t.Fatalf("payload mismatch: %s", got.Payload)
	}
	_ = store.Delete(ctx, "rtest-1")
	if _, err := store.Get(ctx, "rtest-1"); err != sessions.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFactorySelectsBackend(t *testing.T) {
	tests := []struct {
		env     map[string]string
		want    string
		wantErr bool
	}{
		{env: map[string]string{"OMUR_SESSION_BACKEND": "postgres"}, want: "postgres"},
		{env: map[string]string{"OMUR_SESSION_BACKEND": "redis"}, want: "redis"},
		{env: map[string]string{}, want: "postgres"},
		{env: map[string]string{"OMUR_SESSION_BACKEND": "garbage"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.want+"-"+fmtBool(tc.wantErr), func(t *testing.T) {
			t.Setenv("OMUR_SESSION_BACKEND", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got, err := sessions.BackendFromEnv()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err mismatch: %v want=%v", err, tc.wantErr)
			}
			if !tc.wantErr && string(got) != tc.want {
				t.Fatalf("backend: got %s want %s", got, tc.want)
			}
		})
	}
}

func fmtBool(b bool) string {
	if b {
		return "err"
	}
	return "ok"
}
