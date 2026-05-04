// redis.go — redis module.
//
// exports: RedisConfig | NewRedisBus | Publish | PublishTenant | Subscribe | Close
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package eventbus

import (
	"context"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/valkeystream"
)

// RedisConfig configures the Redis/Valkey-backed event bus, wrapping the
// valkeystream package so existing Streams infrastructure is reused.
type RedisConfig struct {
	Addr         string
	Password     string
	StreamPrefix string // e.g. "omur:events:"
	ConsumerName string
	Group        string
}

type redisBus struct {
	cfg     RedisConfig
	streams map[string]*valkeystream.Stream
}

// NewRedisBus returns a Bus backed by Redis Streams via valkeystream. Each
// topic maps to its own stream key prefixed with StreamPrefix.
func NewRedisBus(cfg RedisConfig) (Bus, error) {
	if cfg.StreamPrefix == "" {
		cfg.StreamPrefix = "omur:events:"
	}
	return &redisBus{cfg: cfg, streams: map[string]*valkeystream.Stream{}}, nil
}

func (b *redisBus) streamFor(topic string) (*valkeystream.Stream, error) {
	if s, ok := b.streams[topic]; ok {
		return s, nil
	}
	s, err := valkeystream.New(b.cfg.Addr, b.cfg.Password,
		b.cfg.StreamPrefix+topic, b.cfg.Group, b.cfg.ConsumerName)
	if err != nil {
		return nil, err
	}
	b.streams[topic] = s
	return s, nil
}

func (b *redisBus) Publish(ctx context.Context, topic string, payload []byte) error {
	s, err := b.streamFor(topic)
	if err != nil {
		return err
	}
	_, err = s.Add(ctx, map[string]string{"payload": string(payload)})
	return err
}

// PublishTenant emits a tenant-scoped event on the Redis backend.
// The Redis bus has no RLS; tenant isolation is a property of the Postgres
// backend. Here we embed tenant_id in the stream field so subscribers can
// filter, keeping the Bus contract uniform.
func (b *redisBus) PublishTenant(ctx context.Context, tenantID, topic string, payload []byte) error {
	s, err := b.streamFor(topic)
	if err != nil {
		return err
	}
	_, err = s.Add(ctx, map[string]string{
		"tenant_id": tenantID,
		"payload":   string(payload),
	})
	return err
}

func (b *redisBus) Subscribe(ctx context.Context, topic string, handler Handler) error {
	s, err := b.streamFor(topic)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		msgs, err := s.ReadGroup(ctx, 100, 2*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		for _, m := range msgs {
			e := &Event{
				ID:        0,
				TenantID:  m.Values["tenant_id"],
				Topic:     topic,
				Payload:   []byte(m.Values["payload"]),
				CreatedAt: time.Now(),
			}
			if err := handler(ctx, e); err == nil {
				_ = s.Ack(ctx, m.ID)
			}
		}
	}
}

func (b *redisBus) Close() error {
	for _, s := range b.streams {
		_ = s.Close()
	}
	return nil
}
