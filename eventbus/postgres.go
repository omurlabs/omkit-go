package eventbus

import (
	"context"
	"log/slog"
	"time"

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

func (b *postgresBus) Publish(ctx context.Context, topic string, payload []byte) error {
	_, err := b.pool.Exec(ctx,
		`INSERT INTO events (topic, payload) VALUES ($1, $2)`, topic, payload)
	return err
}

func (b *postgresBus) Subscribe(ctx context.Context, topic string, handler Handler) error {
	if _, err := b.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS event_offsets (
			consumer TEXT NOT NULL,
			topic TEXT NOT NULL,
			last_id BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (consumer, topic)
		)`); err != nil {
		return err
	}
	if _, err := b.pool.Exec(ctx, `
		INSERT INTO event_offsets (consumer, topic, last_id)
		VALUES ($1, $2, 0)
		ON CONFLICT DO NOTHING`, b.cfg.ConsumerName, topic); err != nil {
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
	rows, err := b.pool.Query(ctx, `
		SELECT id, topic, payload, created_at FROM events
		WHERE topic = $1 AND id > (
			SELECT last_id FROM event_offsets WHERE consumer = $2 AND topic = $1
		)
		ORDER BY id ASC LIMIT $3`, topic, b.cfg.ConsumerName, b.cfg.BatchSize)
	if err != nil {
		return err
	}
	defer rows.Close()

	var lastID int64
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Topic, &e.Payload, &e.CreatedAt); err != nil {
			return err
		}
		if err := handler(ctx, &e); err != nil {
			return err
		}
		lastID = e.ID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if lastID > 0 {
		_, err = b.pool.Exec(ctx, `
			UPDATE event_offsets SET last_id = $1
			WHERE consumer = $2 AND topic = $3`, lastID, b.cfg.ConsumerName, topic)
	}
	return err
}

func (b *postgresBus) Close() error {
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	return nil
}
