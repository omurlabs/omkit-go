// consumer.go — consumer module.
//
// exports: ClaimStale | Pending
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package valkeystream

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// ClaimStale uses XAUTOCLAIM to take ownership of messages that have been
// pending longer than minIdle from any consumer in the group.
func (s *Stream) ClaimStale(ctx context.Context, minIdle time.Duration, count int64) ([]Message, error) {
	result, _, err := s.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   s.streamKey,
		Group:    s.group,
		Consumer: s.consumer,
		MinIdle:  minIdle,
		Start:    "0-0",
		Count:    count,
	}).Result()
	if err != nil {
		return nil, err
	}

	msgs := make([]Message, 0, len(result))
	for _, xmsg := range result {
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
	return msgs, nil
}

// Pending returns the XPENDING summary for the stream/group.
func (s *Stream) Pending(ctx context.Context) (*redis.XPending, error) {
	return s.client.XPending(ctx, s.streamKey, s.group).Result()
}
