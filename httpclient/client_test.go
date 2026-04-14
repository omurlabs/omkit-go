package httpclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/httpclient"
)

func TestPostJSON_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := httpclient.New()
	resp, err := c.PostJSON(context.Background(), srv.URL, map[string]string{"hello": "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPostJSON_RetriesOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithRetries(3), httpclient.WithTimeout(5*time.Second))
	resp, err := c.PostJSON(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

func TestPostJSON_RetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithRetries(3), httpclient.WithTimeout(5*time.Second))
	resp, err := c.PostJSON(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error on exhausted retries: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
}

func TestBearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer mytoken" {
			t.Errorf("expected 'Bearer mytoken', got %q", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithBearerToken("mytoken"))
	resp, err := c.PostJSON(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
}

func TestServiceTokenAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("X-Service-Token")
		if tok != "svc-secret" {
			t.Errorf("expected 'svc-secret', got %q", tok)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithServiceToken("svc-secret"))
	resp, err := c.PostJSON(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
}

func TestNotifyAsync(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
		close(done)
	}))
	defer srv.Close()

	c := httpclient.New()
	c.NotifyAsync(context.Background(), srv.URL, map[string]string{"event": "ping"})

	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("NotifyAsync did not reach server within timeout")
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cb := httpclient.NewCircuitBreaker(3, 30*time.Second)
	c := httpclient.New(
		httpclient.WithRetries(1),
		httpclient.WithCircuitBreaker(cb),
		httpclient.WithTimeout(2*time.Second),
	)

	// 3 failures to open the circuit (each call = 1 attempt with WithRetries(1))
	for i := 0; i < 3; i++ {
		resp, err := c.PostJSON(context.Background(), srv.URL, nil)
		if err == nil {
			resp.Body.Close()
		}
	}

	// Next call should return ErrCircuitOpen
	_, err := c.PostJSON(context.Background(), srv.URL, nil)
	if err != httpclient.ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}
