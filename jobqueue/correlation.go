// correlation.go — X-Request-ID correlation propagation across the queue boundary.
//
// G6: spine enqueues, solid-sync/reflex consume — without these helpers
// the request_id is dropped at the queue. Added as a separate file
// (rather than extending envelope.go) so the ctx-key + middleware are
// easy to find by name.
//
// exports: WithRequestID | RequestIDFromContext | EnqueueMiddleware | NewEnqueueMiddleware | WithEnvelope
// rules:   The ctx key is a private struct{} so callers cannot
//          accidentally collide with another package's request_id key.
//          The empty string is the "no correlation id" sentinel —
//          consistent with the omitempty JSON tag in Envelope.

package jobqueue

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tenant"
)

// requestIDCtxKey is the private context key carrying the X-Request-ID
// correlation id across asynq enqueue/consume boundaries.
type requestIDCtxKey struct{}

// WithRequestID returns ctx augmented with the given request id. Pass an
// empty string to clear (which RequestIDFromContext then returns).
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, requestID)
}

// RequestIDFromContext returns the request id stored in ctx, or "" if
// none is set. Callers MUST tolerate the empty string — older inbound
// requests (and tests) carry no correlation id.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// EnqueueMiddleware is a Client-side decorator that lifts the request id
// out of ctx and into the envelope's RequestID field on every Enqueue.
// Call sites that already use ``client.Enqueue(ctx, ...)`` keep working
// unchanged — the middleware just needs to be applied once at construction.
//
// Wire-up pattern (sample, see spine for the canonical site):
//
//	jobClient, err := jobqueue.NewClient(cfg)
//	enqueuer := jobqueue.EnqueueMiddleware(jobClient)
//	// Use ``enqueuer`` everywhere ``jobClient`` was used.
//
// Rules: this middleware does NOT touch tenant_id — that still flows
// through the existing Wrap path. RequestID is purely additive metadata.
type EnqueueMiddleware struct {
	inner *Client
}

// NewEnqueueMiddleware decorates a Client so every Enqueue carries the
// request id from ctx into the envelope.
func NewEnqueueMiddleware(c *Client) *EnqueueMiddleware {
	return &EnqueueMiddleware{inner: c}
}

// Enqueue wraps Client.Enqueue, injecting request_id from ctx into the
// envelope. Falls back to a plain Enqueue (no request_id) when ctx
// carries no correlation id.
func (m *EnqueueMiddleware) Enqueue(ctx context.Context, taskType, tenantID string, payload any, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	requestID := RequestIDFromContext(ctx)
	if requestID == "" {
		// No correlation to propagate — preserve existing wire format.
		return m.inner.Enqueue(ctx, taskType, tenantID, payload, opts...)
	}
	// Build the envelope with the request id, then call asynq directly so
	// we don't double-marshal the payload.
	body, err := WrapWithRequestID(tenantID, requestID, payload)
	if err != nil {
		return nil, err
	}
	task := asynq.NewTask(taskType, body)
	full := append(defaultOptions(), opts...)
	full = append(full, asynq.Queue(m.inner.queue))
	return m.inner.asynq.EnqueueContext(ctx, task, full...)
}

// Close releases the underlying client. Idempotent — mirrors Client.Close
// so callers can swap EnqueueMiddleware in for *Client without rewiring
// shutdown.
func (m *EnqueueMiddleware) Close() error {
	if m == nil || m.inner == nil {
		return nil
	}
	return m.inner.Close()
}

// WithEnvelope is the recommended replacement for WithTenant when the
// caller wants both tenant_id AND request_id propagated to ctx. Behaves
// identically to WithTenant for the tenant side; additionally calls
// WithRequestID(ctx, env.RequestID) so downstream slog/HTTP code sees
// the correlation id.
//
// Existing callers can migrate in-place (one-line swap). Callers that
// don't care about correlation can stay on WithTenant.
func WithEnvelope(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		env, err := Unwrap(t.Payload())
		if err != nil {
			// Mirror WithTenant — chain SkipRetry so a malformed envelope
			// is dead-lettered rather than spinning forever.
			return fmt.Errorf("%w: %w", asynq.SkipRetry, err)
		}
		ctx = tenant.WithID(ctx, env.TenantID)
		if env.RequestID != "" {
			ctx = WithRequestID(ctx, env.RequestID)
		}
		inner := asynq.NewTask(t.Type(), env.Payload)
		return next.ProcessTask(ctx, inner)
	})
}
