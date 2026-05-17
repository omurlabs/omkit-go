// postgres.go — postgres module.
//
// exports: PostgresConfig | NewPostgresBus | Publish | PublishTenant | Subscribe | Close | NilTenantID
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message:

package eventbus

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NilTenantID is the nil-UUID sentinel for system-level events that have no
// tenant context. Matches the pattern used by `security_events` and enforced
// by the events RLS policy in migration 0005: a row stamped with this
// sentinel is readable from a session that has set `app.role = 'admin'`,
// and only writable by a session that has set `app.role = 'service'`. The
// bus uses BYPASSRLS (`SET LOCAL row_security = off`) for the write itself,
// but the row carries the sentinel so downstream admin readers can see it
// without the previous NULL-tenant cross-tenant leak.
const NilTenantID = "00000000-0000-0000-0000-000000000000"

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

// Publish writes a system-level event with the nil-UUID tenant sentinel
// (NilTenantID). Before migration 0005 this method wrote NULL tenant_id;
// the new RLS policy hides NULL rows from every connection except those
// with `SET LOCAL row_security = off` (the bus itself), which left the
// table internally inconsistent — admin-role readers and the tenant-scoped
// `omur_app` read path could no longer see the rows. Writing the sentinel
// closes that asymmetry while keeping the cross-tenant leak fixed.
func (b *postgresBus) Publish(ctx context.Context, topic string, payload []byte) error {
	return b.withBusRole(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO events (tenant_id, topic, payload) VALUES ($1::uuid, $2, $3)`,
			NilTenantID, topic, payload)
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
