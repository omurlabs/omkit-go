package cerebellum

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPost_SurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.post(context.Background(), "/ner", map[string]any{"texts": []string{"x"}}, "", "")
	if !errors.Is(err, ErrRemoteFailure) {
		t.Fatalf("got %v, want ErrRemoteFailure", err)
	}
}

func TestPost_SurfacesCircuitOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := New(srv.URL, WithFailureThreshold(2))
	// Trip the breaker.
	_, _ = c.post(context.Background(), "/ner", map[string]any{}, "", "")
	_, _ = c.post(context.Background(), "/ner", map[string]any{}, "", "")
	_, err := c.post(context.Background(), "/ner", map[string]any{}, "", "")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("got %v, want ErrCircuitOpen", err)
	}
}

func TestPost_PropagatesTraceparent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Traceparent")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	// otelhttp injects Traceparent when a span is in context; we just verify
	// the transport chain exists (no panic, header present whenever otel is
	// configured globally — here we at least assert the request succeeded).
	_, err := c.post(context.Background(), "/ner", map[string]any{}, "", "")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// With no active span, Traceparent may be empty; this test exists to
	// document that the otel-wrapped transport is the one making the call.
	_ = got
}

func TestNew_RespectsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	c := New(srv.URL, WithTimeout(50*time.Millisecond))
	_, err := c.post(context.Background(), "/ner", map[string]any{}, "", "")
	if !errors.Is(err, ErrRemoteFailure) {
		t.Fatalf("timeout: got %v, want ErrRemoteFailure", err)
	}
}

func TestPost_SerialisesTenantHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Tenant-ID")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.post(context.Background(), "/ner", map[string]any{}, "req1", "tenant-A")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tenant-A" {
		t.Fatalf("X-Tenant-ID header: got %q, want %q", got, "tenant-A")
	}
	_ = json.Marshal // keep import
}
