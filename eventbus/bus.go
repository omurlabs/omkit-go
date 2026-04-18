// Package eventbus defines a pluggable pub/sub-style event bus used for
// cross-service notifications (settings changes, provider updates, metrics).
// Backends include PostgresEventBus (polling, default) and RedisEventBus
// (opt-in, wraps valkeystream).
package eventbus

import (
	"context"
	"time"
)

// Event is a single published record.
type Event struct {
	ID        int64
	TenantID  string // empty for global/system events
	Topic     string
	Payload   []byte
	CreatedAt time.Time
}

// Handler is invoked for each delivered event. Return a non-nil error to leave
// the event unacknowledged; the polling loop will retry on the next tick.
type Handler func(ctx context.Context, e *Event) error

// Bus is the backend-agnostic contract for publishing and subscribing to events.
type Bus interface {
	// Publish emits a global/system event (NULL tenant_id). Use
	// PublishTenant for per-tenant events that must be RLS-isolated.
	Publish(ctx context.Context, topic string, payload []byte) error
	// PublishTenant emits an event scoped to a tenant. Subscribers with
	// the same tenant context will see it; others will not (enforced by
	// RLS on the Postgres backend).
	PublishTenant(ctx context.Context, tenantID, topic string, payload []byte) error
	// Subscribe blocks until ctx is cancelled or Close is called, delivering
	// events for the given topic to handler one at a time.
	Subscribe(ctx context.Context, topic string, handler Handler) error
	Close() error
}
