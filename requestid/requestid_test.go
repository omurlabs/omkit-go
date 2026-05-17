package requestid_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omurlabs/omkit-go/requestid"
)

func TestNewContext_RoundTrip(t *testing.T) {
	ctx := requestid.NewContext(context.Background(), "abc-123")
	if got := requestid.FromContext(ctx); got != "abc-123" {
		t.Fatalf("FromContext: got %q, want %q", got, "abc-123")
	}
}

func TestNewContext_EmptyIDIsNoop(t *testing.T) {
	ctx := requestid.NewContext(context.Background(), "")
	if got := requestid.FromContext(ctx); got != "" {
		t.Fatalf("FromContext on empty: got %q, want empty", got)
	}
}

func TestFromContext_MissingReturnsEmpty(t *testing.T) {
	if got := requestid.FromContext(context.Background()); got != "" {
		t.Fatalf("FromContext on bare ctx: got %q, want empty", got)
	}
}

func TestWithRequestIDPropagation_CopiesHeader(t *testing.T) {
	ctx := requestid.NewContext(context.Background(), "req-from-ctx")
	req, _ := http.NewRequest(http.MethodGet, "http://example/", nil)

	requestid.WithRequestIDPropagation(ctx, req)

	if got := req.Header.Get(requestid.HeaderName); got != "req-from-ctx" {
		t.Fatalf("X-Request-ID: got %q, want %q", got, "req-from-ctx")
	}
}

func TestWithRequestIDPropagation_PreservesExistingHeader(t *testing.T) {
	ctx := requestid.NewContext(context.Background(), "ctx-id")
	req, _ := http.NewRequest(http.MethodGet, "http://example/", nil)
	req.Header.Set(requestid.HeaderName, "caller-id")

	requestid.WithRequestIDPropagation(ctx, req)

	if got := req.Header.Get(requestid.HeaderName); got != "caller-id" {
		t.Fatalf("caller header should win: got %q, want %q", got, "caller-id")
	}
}

func TestWithRequestIDPropagation_EmptyCtxIsNoop(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example/", nil)
	requestid.WithRequestIDPropagation(context.Background(), req)
	if got := req.Header.Get(requestid.HeaderName); got != "" {
		t.Fatalf("empty ctx: got %q, want empty", got)
	}
}

func TestWithRequestIDPropagation_NilReqIsNoop(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil req should not panic: %v", r)
		}
	}()
	ctx := requestid.NewContext(context.Background(), "id")
	requestid.WithRequestIDPropagation(ctx, nil)
}

// TestEnd2End_PropagatesAcrossServer documents the intended ingress→egress
// hop: ctx-installed id reaches a downstream test server unchanged.
func TestEnd2End_PropagatesAcrossServer(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(requestid.HeaderName)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := requestid.NewContext(context.Background(), "550e8400-e29b-41d4-a716-446655440000")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	requestid.WithRequestIDPropagation(ctx, req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	resp.Body.Close()

	if got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("server saw X-Request-ID=%q, want propagated id", got)
	}
}
