package registry_test

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omur-core/packages/omur-go-sdk/registry"
)

type countingProvider struct {
	runs *atomic.Int64
}

func (c *countingProvider) Run(ctx context.Context) error {
	c.runs.Add(1)
	<-ctx.Done()
	return nil
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

func TestRegistryBackendFromEnvDefaultPostgres(t *testing.T) {
	t.Setenv("OMUR_PROVIDERS_BACKEND", "")
	reg := registry.New("collector", nil, "", nil)
	if reg == nil {
		t.Fatal("nil registry")
	}
}

func TestRegistryPollingPicksUpProviders(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a tenant and provider row that will be polled.
	tenantID := "00000000-0000-0000-0000-00000000bbbb"
	if _, err := pool.Exec(ctx, `
		INSERT INTO tenants (id, name) VALUES ($1, 'registry-test-tenant')
		ON CONFLICT (id) DO NOTHING`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	defer pool.Exec(context.Background(), "DELETE FROM providers WHERE tenant_id = $1 AND kind = 'collector-test'", tenantID)
	defer pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID)

	var runs atomic.Int64
	factories := map[string]registry.ProviderFactory{
		"test-provider": func(tid string, cfg map[string]any) registry.Provider {
			return &countingProvider{runs: &runs}
		},
	}

	t.Setenv("OMUR_PROVIDERS_BACKEND", "postgres")
	reg := registry.New("collector-test", pool, "", factories,
		registry.WithPollInterval(100*time.Millisecond))
	reg.Start(ctx)
	defer reg.Stop()

	// Insert the provider row AFTER Start — polling must detect it.
	_, err := pool.Exec(ctx, `
		INSERT INTO providers (tenant_id, kind, name, enabled, config)
		VALUES ($1, 'collector-test', 'test-provider', true, '{}'::jsonb)`,
		tenantID)
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runs.Load() > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("polling did not pick up provider; runs=%d running=%v", runs.Load(), reg.RunningKeys())
}
