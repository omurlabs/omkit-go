package httpclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

func TestDo_ForwardsMethodAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.Header.Get("X-Custom") != "yes" {
			t.Errorf("expected X-Custom=yes, got %q", r.Header.Get("X-Custom"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New()
	req, _ := http.NewRequest(http.MethodPut, srv.URL, nil)
	req.Header.Set("X-Custom", "yes")
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	resp.Body.Close()
}

func TestDo_CallerAuthWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer caller" {
			t.Errorf("expected Bearer caller, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithBearerToken("sdk"))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer caller")
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	resp.Body.Close()
}

func TestDo_SDKAuthAppliedWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sdk" {
			t.Errorf("expected Bearer sdk, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithBearerToken("sdk"))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	resp.Body.Close()
}

func TestDo_CircuitBreakerTripsOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cb := httpclient.NewCircuitBreaker(3, 30*time.Second)
	c := httpclient.New(httpclient.WithCircuitBreaker(cb), httpclient.WithTimeout(2*time.Second))
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, err := c.Do(context.Background(), req)
		if err == nil {
			resp.Body.Close()
		}
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := c.Do(context.Background(), req)
	if err != httpclient.ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestDo_StreamBody_NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Read and discard body — proves it was actually sent on this attempt.
		body, _ := io.ReadAll(r.Body)
		if string(body) != "body" {
			t.Errorf("expected body=%q, got %q", "body", body)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithRetries(5)) // retries should NOT apply to Do
	req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader([]byte("body")))
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	defer resp.Body.Close()

	// Strong assertions: exactly one call AND the 5xx response propagates.
	if calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d (Do unexpectedly retried)", calls.Load())
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 propagated to caller, got %d", resp.StatusCode)
	}
}

func TestDo_HeaderIsolation_DoesNotMutateCallerRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithBearerToken("sdk-default"))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("precondition: req.Header should be empty, got %q", got)
	}

	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// SDK should NOT have mutated caller's original request headers.
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Do leaked SDK header back to caller's req: %q", got)
	}
}

func TestDo_TransportError_ReturnNilResp(t *testing.T) {
	c := httpclient.New(httpclient.WithTimeout(50 * time.Millisecond))
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1", nil) // closed
	resp, err := c.Do(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if resp != nil {
		t.Errorf("expected nil resp, got %v", resp)
	}
}

func TestDo_NonTwoxxBodyNotClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()

	c := httpclient.New()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"error":"nope"}` {
		t.Errorf("expected error body readable, got %q", body)
	}
}

func TestDo_CheckRedirect_ErrUseLastResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://evil.example/")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithCheckRedirect(func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "http://evil.example/" {
		t.Errorf("expected raw Location header preserved, got %q", loc)
	}
}

func TestDo_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(httpclient.WithTimeout(time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := c.Do(ctx, req)
	if err == nil {
		t.Fatal("expected ctx error")
	}
}
