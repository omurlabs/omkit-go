package tenant

import (
	"context"
	"net/http"
)

// WithID returns a child ctx whose tenant ID is the given value, overriding any
// previously-set value. Typed the same way Middleware() attaches it, so the rest
// of the tenant package (FromContext, FromRequest, Require) sees it transparently.
//
// Use this when a handler is mounted under an admin-only prefix and must act
// on a specific tenant regardless of the caller's resolved tenant — e.g., the
// admin wrappers that operate on the system tenant.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// WithIDRequest returns a new *http.Request whose context carries the given
// tenant ID. The returned request points to the same underlying body/headers;
// only the context is replaced.
func WithIDRequest(r *http.Request, id string) *http.Request {
	return r.WithContext(WithID(r.Context(), id))
}
