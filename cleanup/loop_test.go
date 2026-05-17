package cleanup_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omkit-go/cleanup"
	"github.com/omurlabs/omkit-go/requestid"
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

func TestLoop_TickInjectsWorkerContext(t *testing.T) {
	pool := testPool(t)

	var (
		mu           sync.Mutex
		gotRequestID string
	)
	loop := cleanup.NewLoop(pool, cleanup.Config{
		LockKey:  9_999_001,
		Interval: 100 * time.Millisecond,
		Name:     "test-worker",
		Task: func(ctx context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			if gotRequestID == "" {
				gotRequestID = requestid.FromContext(ctx)
			}
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	loop.Run(ctx)

	mu.Lock()
	id := gotRequestID
	mu.Unlock()
	if id == "" {
		t.Fatalf("expected request_id in tick ctx; got empty")
	}
}

func TestLoop_TickEmitsWorker(t *testing.T) {
	pool := testPool(t)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "test-svc"))
	t.Cleanup(func() { slog.SetDefault(prev) })

	loop := cleanup.NewLoop(pool, cleanup.Config{
		LockKey:  9_999_002,
		Interval: 100 * time.Millisecond,
		Name:     "emit-worker",
		Task: func(_ context.Context) error {
			return errors.New("boom") // forces the loop's failure log path
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	loop.Run(ctx)

	if !strings.Contains(buf.String(), `"worker":"emit-worker"`) {
		t.Fatalf("expected worker=emit-worker in failure log; got %q", buf.String())
	}
}
