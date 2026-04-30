// ratelimit.go — ratelimit module.
//
// exports: IPRateLimiter | NewIPRateLimiter | Wrap
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package middleware — IP-keyed token-bucket rate limiter.
//
// In-memory, per-process. Not clustered — each service replica enforces its
// own quota. If an endpoint needs a global budget, divide the desired total
// by the replica count when setting capacity.
//
// The limiter is intentionally simple: the bucket resets to full capacity
// once the window elapses since the first hit, rather than amortizing.
// That matches how a login throttle should feel — 10 attempts then a full
// window of cool-down — and avoids the complexity of a sliding-window or
// leaky-bucket implementation.
package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	tokens  int
	resetAt time.Time
}

// IPRateLimiter caps requests per client IP within a rolling window.
// Safe for concurrent use.
type IPRateLimiter struct {
	capacity int
	window   time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

// NewIPRateLimiter returns an IPRateLimiter that allows `capacity` hits per
// `window` per client IP. The bucket resets fully once the window elapses.
func NewIPRateLimiter(capacity int, window time.Duration) *IPRateLimiter {
	return &IPRateLimiter{
		capacity: capacity,
		window:   window,
		buckets:  make(map[string]*bucket),
	}
}

// allow reports whether the caller at ip may consume a token right now.
func (l *IPRateLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[ip]
	if !ok || now.After(b.resetAt) {
		l.buckets[ip] = &bucket{tokens: l.capacity - 1, resetAt: now.Add(l.window)}
		return true
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// Wrap returns an http.Handler that denies requests from IPs over quota with
// HTTP 429 + Retry-After: <seconds of window>. The handler passes allowed
// requests to next unchanged.
func (l *IPRateLimiter) Wrap(next http.Handler) http.Handler {
	retryAfter := strconv.Itoa(int(l.window.Seconds()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", retryAfter)
			http.Error(w, "rate_limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the caller's IP, preferring the first entry in
// X-Forwarded-For (trusted when the service sits behind Caddy) and falling
// back to the TCP RemoteAddr. The port is always stripped.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma >= 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
