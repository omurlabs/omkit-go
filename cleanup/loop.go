// loop.go — loop module.
//
// exports: Config | Loop | NewLoop | Run
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package cleanup provides a coordinated periodic task runner. Loop.Run
// fires Config.Task every Config.Interval while holding a
// `pg_try_advisory_lock(Config.LockKey)` — so horizontally scaled replicas
// of the same service don't double-execute the task. When the lock is held
// elsewhere the tick is a no-op.
package cleanup

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	LockKey  int64                           // pg_try_advisory_lock key — pick one globally unique per task
	Interval time.Duration                   // how often to run; default 60s
	Task     func(ctx context.Context) error // the cleanup work
	Name     string                          // for log lines; defaults to "cleanup-loop"
}

type Loop struct {
	pool *pgxpool.Pool
	cfg  Config
}

// NewLoop returns a Loop that hasn't started yet; call Run(ctx) to start it.
func NewLoop(pool *pgxpool.Pool, cfg Config) *Loop {
	if cfg.Interval == 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.Name == "" {
		cfg.Name = "cleanup-loop"
	}
	return &Loop{pool: pool, cfg: cfg}
}

// Run blocks until ctx is cancelled, invoking Task every Interval whenever
// the advisory lock is acquired. Lock contention yields a silent no-op tick.
func (l *Loop) Run(ctx context.Context) {
	t := time.NewTicker(l.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.tick(ctx)
		}
	}
}

func (l *Loop) tick(ctx context.Context) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		slog.Warn(l.cfg.Name+": acquire failed", "err", err)
		return
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx,
		"SELECT pg_try_advisory_lock($1)", l.cfg.LockKey).Scan(&got); err != nil {
		slog.Warn(l.cfg.Name+": lock query failed", "err", err)
		return
	}
	if !got {
		return
	}
	defer func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", l.cfg.LockKey)
	}()

	if err := l.cfg.Task(ctx); err != nil {
		slog.Warn(l.cfg.Name+": task failed", "err", err)
	}
}
