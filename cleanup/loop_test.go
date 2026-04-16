package cleanup_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omur-core/packages/omur-go-sdk/cleanup"
)

func testPool(t *testing.T) *pgxpool.Pool {
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

func TestLoopAcquiresAdvisoryLockAndRunsTask(t *testing.T) {
	pool := testPool(t)
	ran := make(chan struct{}, 4)
	loop := cleanup.NewLoop(pool, cleanup.Config{
		LockKey:  9999,
		Interval: 50 * time.Millisecond,
		Task: func(ctx context.Context) error {
			ran <- struct{}{}
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go loop.Run(ctx)

	select {
	case <-ran:
	case <-time.After(900 * time.Millisecond):
		t.Fatal("task never ran")
	}
}

func TestLoopHonorsLockContention(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(8888)"); err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock(8888)")

	ran := make(chan struct{}, 4)
	loop := cleanup.NewLoop(pool, cleanup.Config{
		LockKey:  8888,
		Interval: 30 * time.Millisecond,
		Task: func(ctx context.Context) error {
			ran <- struct{}{}
			return nil
		},
	})
	loopCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	go loop.Run(loopCtx)

	select {
	case <-ran:
		t.Fatal("task ran despite lock held elsewhere")
	case <-time.After(250 * time.Millisecond):
	}
}
