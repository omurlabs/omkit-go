// server.go — server module.
//
// exports: NewServer | WithTenant | WithTenantFunc | IsInvalidEnvelopeError
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package jobqueue

import (
	"context"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tenant"
)

// NewServer builds an Asynq server pinned to cfg.QueueName with cfg.Concurrency
// workers. Caller registers handlers on the returned *asynq.ServeMux and then
// calls Server.Start (non-blocking) or Server.Run (blocking).
//
// SDK convention: services use Start so they own their own signal-handling
// loop. Run is reserved for standalone daemons that have no other lifecycle.
func NewServer(cfg Config) (*asynq.Server, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     cfg.Addr,
			Password: cfg.Password,
		},
		asynq.Config{
			Concurrency:     cfg.concurrency(),
			Queues:          map[string]int{cfg.QueueName: 1},
			ShutdownTimeout: cfg.ShutdownTimeout,
		},
	)
	return srv, nil
}

// WithTenant is an asynq middleware that unwraps the SDK Envelope, attaches
// the tenant_id to ctx (via the omur-go-sdk tenant package), and invokes the
// next handler with the rewritten task whose payload is the inner JSON.
//
// Invalid envelopes are dead-lettered (asynq.SkipRetry) — retrying a malformed
// envelope can never succeed.
func WithTenant(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		env, err := Unwrap(t.Payload())
		if err != nil {
			// SkipRetry tells Asynq to archive immediately rather than retry.
			// Chain both sentinels so callers can errors.Is for either.
			return fmt.Errorf("%w: %w", asynq.SkipRetry, err)
		}
		ctx = tenant.WithID(ctx, env.TenantID)
		// Hand the handler the inner payload so it doesn't need to know about
		// the envelope. ResultWriter is preserved by the original task pointer.
		inner := asynq.NewTask(t.Type(), env.Payload)
		return next.ProcessTask(ctx, inner)
	})
}

// WithTenantFunc is the HandlerFunc-typed convenience wrapper.
func WithTenantFunc(h asynq.HandlerFunc) asynq.HandlerFunc {
	wrapped := WithTenant(h)
	return func(ctx context.Context, t *asynq.Task) error {
		return wrapped.ProcessTask(ctx, t)
	}
}

// IsInvalidEnvelopeError reports whether err originated from envelope
// validation. Callers can use it to record dead-letter metrics distinct from
// handler-level errors.
func IsInvalidEnvelopeError(err error) bool {
	return errors.Is(err, ErrInvalidEnvelope)
}
