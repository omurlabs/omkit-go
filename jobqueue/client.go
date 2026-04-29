package jobqueue

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
)

// Client is a thin wrapper over *asynq.Client that injects the SDK envelope
// and default options on every Enqueue. Construct one per service and reuse —
// the underlying asynq.Client owns its own Redis connection pool.
type Client struct {
	asynq *asynq.Client
	queue string
}

// NewClient validates cfg, builds the underlying asynq.Client, and returns a
// Client bound to cfg.QueueName. Caller is responsible for calling Close on
// shutdown.
func NewClient(cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	c := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     cfg.Addr,
		Password: cfg.Password,
	})
	return &Client{asynq: c, queue: cfg.QueueName}, nil
}

// Enqueue marshals payload into an Envelope and pushes the task onto the
// configured queue. SDK defaults (MaxRetry, Timeout, Retention) are applied
// first; caller-supplied opts override.
//
// taskType identifies the handler that will run, e.g. "solid-sync:fhir-push".
// tenantID must be a valid UUID — invalid envelopes are rejected before the
// task ever reaches Valkey.
func (c *Client) Enqueue(ctx context.Context, taskType, tenantID string, payload any, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	task, defaults, err := NewTask(taskType, tenantID, payload, opts...)
	if err != nil {
		return nil, err
	}
	// Bind to this client's queue last so caller can't override it accidentally.
	full := append(defaults, asynq.Queue(c.queue))
	info, err := c.asynq.EnqueueContext(ctx, task, full...)
	if err != nil {
		return nil, fmt.Errorf("asynq enqueue: %w", err)
	}
	return info, nil
}

// Close releases the underlying asynq.Client. Idempotent.
func (c *Client) Close() error {
	if c == nil || c.asynq == nil {
		return nil
	}
	return c.asynq.Close()
}
