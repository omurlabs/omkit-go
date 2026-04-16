package eventbus_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omurlabs/omur-core/packages/omur-go-sdk/eventbus"
)

func TestEventInterface(t *testing.T) {
	var _ eventbus.Bus = (*fakeBus)(nil)
}

type fakeBus struct{}

func (f *fakeBus) Publish(ctx context.Context, topic string, payload []byte) error { return nil }
func (f *fakeBus) Subscribe(ctx context.Context, topic string, handler eventbus.Handler) error {
	return nil
}
func (f *fakeBus) Close() error { return nil }

func TestEventStruct(t *testing.T) {
	e := &eventbus.Event{
		ID:        1,
		Topic:     "x",
		Payload:   []byte(`{}`),
		CreatedAt: time.Now(),
	}
	if e.Topic != "x" {
		t.Fatal("topic")
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

func TestPostgresBusPublishSubscribe(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events (
			id BIGSERIAL PRIMARY KEY,
			topic TEXT NOT NULL,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			consumed BOOLEAN NOT NULL DEFAULT false
		)`)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer pool.Exec(context.Background(), "TRUNCATE events RESTART IDENTITY")
	defer pool.Exec(context.Background(), "DELETE FROM event_offsets WHERE consumer = 'test-consumer'")

	bus := eventbus.NewPostgresBus(pool, eventbus.PostgresConfig{
		ConsumerName: "test-consumer",
		PollInterval: 100 * time.Millisecond,
	})
	defer bus.Close()

	if err := bus.Publish(ctx, "test.topic", []byte(`{"k":"v"}`)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	received := make(chan *eventbus.Event, 1)
	go bus.Subscribe(ctx, "test.topic", func(ctx context.Context, e *eventbus.Event) error {
		received <- e
		return nil
	})

	select {
	case e := <-received:
		if e.Topic != "test.topic" {
			t.Fatalf("topic: %s", e.Topic)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive event")
	}
}

func TestRedisBusPublishSubscribe(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	bus, err := eventbus.NewRedisBus(eventbus.RedisConfig{
		Addr:         addr,
		Password:     os.Getenv("TEST_REDIS_PASSWORD"),
		StreamPrefix: "test:omur:events:",
		ConsumerName: "test-consumer",
		Group:        "test-group",
	})
	if err != nil {
		t.Fatalf("NewRedisBus: %v", err)
	}
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bus.Publish(ctx, "redistest", []byte(`{"r":1}`)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	received := make(chan *eventbus.Event, 1)
	go bus.Subscribe(ctx, "redistest", func(ctx context.Context, e *eventbus.Event) error {
		received <- e
		return nil
	})
	select {
	case e := <-received:
		if string(e.Payload) != `{"r":1}` {
			t.Fatalf("payload: %s", e.Payload)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("no event received")
	}
}
