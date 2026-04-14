// Package valkeysub provides a Redis pub/sub subscriber with automatic reconnection.
package valkeysub

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// MessageHandler is called for each received message payload.
type MessageHandler func(payload string)

// Option configures a Subscriber.
type Option func(*Subscriber)

// WithReconnectDelay sets the delay between reconnect attempts.
func WithReconnectDelay(d time.Duration) Option {
	return func(s *Subscriber) {
		s.reconnectDelay = d
	}
}

// Subscriber subscribes to a Redis channel and dispatches messages.
type Subscriber struct {
	addr           string
	reconnectDelay time.Duration
}

// New creates a new Subscriber for the given Redis address.
func New(addr string, opts ...Option) *Subscriber {
	s := &Subscriber{
		addr:           addr,
		reconnectDelay: 5 * time.Second,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ReconnectDelay returns the configured reconnect delay.
func (s *Subscriber) ReconnectDelay() time.Duration {
	return s.reconnectDelay
}

// Subscribe blocks and dispatches messages from the given channel to handler.
// It automatically reconnects on error. Returns when ctx is cancelled.
func (s *Subscriber) Subscribe(ctx context.Context, channel string, handler MessageHandler) {
	for {
		if err := s.run(ctx, channel, handler); err == nil {
			return // context cancelled cleanly
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.reconnectDelay):
		}
	}
}

func (s *Subscriber) run(ctx context.Context, channel string, handler MessageHandler) error {
	client := redis.NewClient(&redis.Options{Addr: s.addr})
	defer client.Close()

	sub := client.Subscribe(ctx, channel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return context.DeadlineExceeded // channel closed — trigger reconnect
			}
			handler(msg.Payload)
		}
	}
}
