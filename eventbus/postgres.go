package eventbus

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresConfig configures the Postgres-backed polling event bus.
type PostgresConfig struct {
	ConsumerName string        // unique per consumer process; used to track offset
	PollInterval time.Duration // default 5s
	BatchSize    int           // default 100
}

type postgresBus struct {
	pool *pgxpool.Pool
	cfg  PostgresConfig
	done chan struct{}
}

// NewPostgresBus returns a Bus that stores events in the `events` table and
// delivers them to subscribers by polling every PollInterval. Each consumer's
// last-seen offset is tracked in `event_offsets` keyed by (consumer, topic).
func NewPostgresBus(pool *pgxpool.Pool, cfg PostgresConfig) Bus {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	return &postgresBus{pool: pool, cfg: cfg, done: make(chan struct{})}
}

// withBusRole runs fn inside a tx on a connection that has dropped the per-
// connection omur_app role and disabled row_security. This lets eventbus
// infrastructure (writes to the global events table, cross-tenant polling in
// Subscribe) operate without being filtered by tenant RLS. The pool owner
// must be a superuser or a BYPASSRLS role — the pattern mirrors
// spine/tenant_quota_metrics.go's scrapeTenantQuotas.
func (b *postgresBus) withBusRole(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "RESET ROLE"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (b *postgresBus) Publish(ctx context.Context, topic string, payload []byte) error {
	return b.withBusRole(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO events (topic, payload) VALUES ($1, $2)`, topic, payload)
		return err
	})
}

// PublishTenant inserts an event bound to tenantID. Subscribers delivered
// via Subscribe will see the row regardless of their session tenant
// context (bus poller bypasses RLS), but any user-facing read path that
// queries events under omur_app is gated by the tenant RLS policy.
func (b *postgresBus) PublishTenant(ctx context.Context, tenantID, topic string, payload []byte) error {
	if tenantID == "" {
		return b.Publish(ctx, topic, payload)
	}
	return b.withBusRole(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO events (tenant_id, topic, payload) VALUES ($1::uuid, $2, $3)`,
			tenantID, topic, payload)
		return err
	})
}

func (b *postgresBus) Subscribe(ctx context.Context, topic string, handler Handler) error {
	if err := b.withBusRole(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS event_offsets (
				consumer TEXT NOT NULL,
				topic TEXT NOT NULL,
				last_id BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (consumer, topic)
			)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO event_offsets (consumer, topic, last_id)
			VALUES ($1, $2, 0)
			ON CONFLICT DO NOTHING`, b.cfg.ConsumerName, topic)
		return err
	}); err != nil {
		return err
	}

	// Drain once immediately so tests and short-lived consumers don't have to
	// wait a full PollInterval for the first delivery.
	if err := b.pollOnce(ctx, topic, handler); err != nil {
		slog.Warn("eventbus: initial poll failed", "topic", topic, "err", err)
	}

	ticker := time.NewTicker(b.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.done:
			return nil
		case <-ticker.C:
			if err := b.pollOnce(ctx, topic, handler); err != nil {
				slog.Warn("eventbus: poll failed", "topic", topic, "err", err)
			}
		}
	}
}

func (b *postgresBus) pollOnce(ctx context.Context, topic string, handler Handler) error {
	// The bus is infrastructure — cleanup loops, settings fanout — and must
	// see every tenant's rows on the subscribed topic. withBusRole drops
	// omur_app + row_security for the duration of this poll.
	var events []Event
	if err := b.withBusRole(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, COALESCE(tenant_id::text, ''), topic, payload, created_at FROM events
			WHERE topic = $1 AND id > (
				SELECT last_id FROM event_offsets WHERE consumer = $2 AND topic = $1
			)
			ORDER BY id ASC LIMIT $3`, topic, b.cfg.ConsumerName, b.cfg.BatchSize)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.ID, &e.TenantID, &e.Topic, &e.Payload, &e.CreatedAt); err != nil {
				return err
			}
			events = append(events, e)
		}
		return rows.Err()
	}); err != nil {
		return err
	}

	var lastID int64
	for _, e := range events {
		if err := handler(ctx, &e); err != nil {
			return err
		}
		lastID = e.ID
	}
	if lastID > 0 {
		return b.withBusRole(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				UPDATE event_offsets SET last_id = $1
				WHERE consumer = $2 AND topic = $3`, lastID, b.cfg.ConsumerName, topic)
			return err
		})
	}
	return nil
}

func (b *postgresBus) Close() error {
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	return nil
}
