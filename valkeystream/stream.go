// stream.go — stream module.
//
// exports: Message | Stream | New | Add | ReadGroup | Ack | Delete | Len | Close | Client
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package valkeystream provides a Redis Streams wrapper with consumer group support.
package valkeystream

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Message represents a single Redis Stream entry.
type Message struct {
	ID     string
	Values map[string]string
}

// Stream wraps a Redis client with stream/consumer-group context.
type Stream struct {
	client    *redis.Client
	streamKey string
	group     string
	consumer  string
}

// New creates a Redis client, verifies connectivity, and ensures the consumer
// group exists (XGROUP CREATE … MKSTREAM). "BUSYGROUP" errors are ignored.
func New(addr, password, streamKey, group, consumer string) (*Stream, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}

	err := client.XGroupCreateMkStream(ctx, streamKey, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		client.Close()
		return nil, err
	}

	return &Stream{
		client:    client,
		streamKey: streamKey,
		group:     group,
		consumer:  consumer,
	}, nil
}

// Add appends values to the stream (XADD) and returns the generated message ID.
func (s *Stream) Add(ctx context.Context, values map[string]string) (string, error) {
	args := &redis.XAddArgs{
		Stream: s.streamKey,
		ID:     "*",
		Values: values,
	}
	return s.client.XAdd(ctx, args).Result()
}

// ReadGroup reads up to count messages from the consumer group (XREADGROUP ">").
// timeout controls the blocking duration; 0 means non-blocking.
func (s *Stream) ReadGroup(ctx context.Context, count int64, timeout time.Duration) ([]Message, error) {
	args := &redis.XReadGroupArgs{
		Group:    s.group,
		Consumer: s.consumer,
		Streams:  []string{s.streamKey, ">"},
		Count:    count,
		Block:    timeout,
	}
	result, err := s.client.XReadGroup(ctx, args).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var msgs []Message
	for _, stream := range result {
		for _, xmsg := range stream.Messages {
			m := Message{
				ID:     xmsg.ID,
				Values: make(map[string]string, len(xmsg.Values)),
			}
			for k, v := range xmsg.Values {
				if sv, ok := v.(string); ok {
					m.Values[k] = sv
				}
			}
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

// Ack acknowledges a message (XACK).
func (s *Stream) Ack(ctx context.Context, id string) error {
	return s.client.XAck(ctx, s.streamKey, s.group, id).Err()
}

// Delete removes a message from the stream (XDEL).
func (s *Stream) Delete(ctx context.Context, id string) error {
	return s.client.XDel(ctx, s.streamKey, id).Err()
}

// Len returns the number of entries in the stream (XLEN).
func (s *Stream) Len(ctx context.Context) (int64, error) {
	return s.client.XLen(ctx, s.streamKey).Result()
}

// Close closes the underlying Redis client.
func (s *Stream) Close() error {
	return s.client.Close()
}

// Client exposes the underlying *redis.Client for direct access.
func (s *Stream) Client() *redis.Client {
	return s.client
}
