package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPTokenBucket_Enforces10PerMinute(t *testing.T) {
	rl := NewIPRateLimiter(10, time.Minute)
	h := rl.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	var accepted, rejected int
	for i := 0; i < 12; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.RemoteAddr = "10.0.0.1:443"
		h.ServeHTTP(rec, req)
		switch rec.Code {
		case http.StatusNoContent:
			accepted++
		case http.StatusTooManyRequests:
			rejected++
			if got := rec.Header().Get("Retry-After"); got == "" {
				t.Errorf("request %d: 429 missing Retry-After header", i)
			}
		default:
			t.Fatalf("request %d: unexpected status %d", i, rec.Code)
		}
	}
	if accepted != 10 || rejected != 2 {
		t.Fatalf("want 10 accepted + 2 rejected, got %d + %d", accepted, rejected)
	}
}

func TestIPTokenBucket_SeparateIPsHaveSeparateBuckets(t *testing.T) {
	rl := NewIPRateLimiter(2, time.Minute)
	h := rl.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	hit := func(ip string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.RemoteAddr = ip + ":80"
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Exhaust IP A.
	for i := 0; i < 2; i++ {
		if got := hit("10.0.0.1"); got != http.StatusNoContent {
			t.Fatalf("A req %d: got %d, want 204", i, got)
		}
	}
	if got := hit("10.0.0.1"); got != http.StatusTooManyRequests {
		t.Fatalf("A overflow: got %d, want 429", got)
	}
	// IP B still has its full quota.
	for i := 0; i < 2; i++ {
		if got := hit("10.0.0.2"); got != http.StatusNoContent {
			t.Fatalf("B req %d: got %d, want 204", i, got)
		}
	}
}

func TestIPTokenBucket_XForwardedForPreferred(t *testing.T) {
	rl := NewIPRateLimiter(1, time.Minute)
	h := rl.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Two requests from different RemoteAddr but same X-Forwarded-For client IP
	// must share a bucket — otherwise an attacker behind Caddy gets unlimited
	// attempts just by cycling source ports.
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.RemoteAddr = "172.17.0.1:" + []string{"40000", "50000"}[i]
		req.Header.Set("X-Forwarded-For", "203.0.113.5, 172.17.0.1")
		h.ServeHTTP(rec, req)
		wantStatus := http.StatusNoContent
		if i == 1 {
			wantStatus = http.StatusTooManyRequests
		}
		if rec.Code != wantStatus {
			t.Fatalf("req %d: got %d, want %d", i, rec.Code, wantStatus)
		}
	}
}
