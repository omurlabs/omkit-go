// client.go — client module.
//
// exports: ErrCircuitOpen | CircuitBreaker | NewCircuitBreaker | Allow | RecordSuccess | RecordFailure | Client | Option | New | WithoutTracing | WithTimeout | WithRetries | WithBearerToken | WithServiceToken | WithTenantHeaderFromContext | WithCircuitBreaker | WithCheckRedirect | WithTransport | PostJSON | GetJSON | (+2 more)
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package httpclient provides an HTTP client with retry, auth, and circuit breaker support.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tenant"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// circuitState represents the current state of the circuit breaker.
type circuitState int

const (
	stateClosed   circuitState = iota
	stateOpen     circuitState = iota
	stateHalfOpen circuitState = iota
)

// CircuitBreaker tracks consecutive failures and opens after a threshold.
type CircuitBreaker struct {
	mu           sync.Mutex
	threshold    int
	cooldown     time.Duration
	failures     int
	state        circuitState
	openedAt     time.Time
}

// NewCircuitBreaker creates a CircuitBreaker that opens after threshold consecutive
// failures and attempts half-open after cooldown.
func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		cooldown:  cooldown,
		state:     stateClosed,
	}
}

// Allow returns true if the request should proceed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case stateClosed:
		return true
	case stateOpen:
		if time.Since(cb.openedAt) >= cb.cooldown {
			cb.state = stateHalfOpen
			return true
		}
		return false
	case stateHalfOpen:
		return true
	}
	return true
}

// RecordSuccess resets the circuit breaker to closed.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = stateClosed
}

// RecordFailure increments the failure count and opens the circuit if threshold reached.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.failures >= cb.threshold {
		cb.state = stateOpen
		cb.openedAt = time.Now()
	}
}

// Client is an HTTP client with retries, auth headers, and optional circuit breaker.
type Client struct {
	http              http.Client
	retries           int
	headers           map[string]string
	circuitBreaker    *CircuitBreaker
	tracingDisabled   bool
	tenantFromContext bool
}

// Option is a functional option for Client.
type Option func(*Client)

// New creates a Client with the given options.
func New(opts ...Option) *Client {
	c := &Client{
		http:    http.Client{Timeout: 30 * time.Second},
		retries: 1,
		headers: make(map[string]string),
	}
	for _, o := range opts {
		o(c)
	}
	if !c.tracingDisabled {
		base := c.http.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		c.http.Transport = otelhttp.NewTransport(base)
	}
	return c
}

// WithoutTracing disables the default otelhttp transport wrapper.
// Use when outbound trace propagation is not desired.
func WithoutTracing() Option {
	return func(c *Client) { c.tracingDisabled = true }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// WithRetries sets the maximum number of attempts (1 = no retry).
func WithRetries(n int) Option {
	return func(c *Client) { c.retries = n }
}

// WithBearerToken sets the Authorization: Bearer <token> header.
func WithBearerToken(token string) Option {
	return func(c *Client) { c.headers["Authorization"] = "Bearer " + token }
}

// WithServiceToken sets the X-Service-Token header.
func WithServiceToken(token string) Option {
	return func(c *Client) { c.headers["X-Service-Token"] = token }
}

// WithTenantHeaderFromContext auto-sets X-Tenant-ID on every outbound request
// from tenant.FromContext(ctx). No-op when the context has no tenant. Intended
// for service-to-service callers that proxy a per-request tenant (e.g. Spine's
// voice proxy, Reflex → Auris, Marrow → Cerebellum).
func WithTenantHeaderFromContext() Option {
	return func(c *Client) { c.tenantFromContext = true }
}

// WithCircuitBreaker attaches a circuit breaker to the client.
func WithCircuitBreaker(cb *CircuitBreaker) Option {
	return func(c *Client) { c.circuitBreaker = cb }
}

// WithCheckRedirect overrides the underlying http.Client.CheckRedirect.
// Use http.ErrUseLastResponse to disable redirect following entirely.
func WithCheckRedirect(fn func(req *http.Request, via []*http.Request) error) Option {
	return func(c *Client) { c.http.CheckRedirect = fn }
}

// WithTransport sets a custom base transport. When tracing is enabled (the
// default), otelhttp wraps this transport so OTel headers are still injected.
func WithTransport(t http.RoundTripper) Option {
	return func(c *Client) { c.http.Transport = t }
}

// PostJSON marshals body as JSON and POSTs it. Retries on 5xx with exponential backoff.
// Returns the last response on retry exhaustion. Returns ErrCircuitOpen if circuit is open.
func (c *Client) PostJSON(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	if c.circuitBreaker != nil && !c.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}

	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("httpclient: marshal body: %w", err)
		}
	}

	var (
		resp *http.Response
		err  error
	)

	backoff := 100 * time.Millisecond
	for attempt := 0; attempt < c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		var req *http.Request
		var bodyReader *bytes.Reader
		if len(buf) > 0 {
			bodyReader = bytes.NewReader(buf)
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("httpclient: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		if c.tenantFromContext {
			if tid := tenant.FromContext(ctx); tid != "" {
				req.Header.Set("X-Tenant-ID", tid)
			}
		}

		resp, err = c.http.Do(req)
		if err != nil {
			if c.circuitBreaker != nil {
				c.circuitBreaker.RecordFailure()
			}
			continue
		}

		if resp.StatusCode >= 500 && attempt < c.retries-1 {
			_ = resp.Body.Close()
			if c.circuitBreaker != nil {
				c.circuitBreaker.RecordFailure()
			}
			continue
		}

		// Final attempt or non-5xx: record outcome
		if c.circuitBreaker != nil {
			if resp.StatusCode >= 500 {
				c.circuitBreaker.RecordFailure()
			} else {
				c.circuitBreaker.RecordSuccess()
			}
		}
		return resp, nil
	}

	// All attempts exhausted with connection errors
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// GetJSON performs a GET request with auth headers.
func (c *Client) GetJSON(ctx context.Context, url string) (*http.Response, error) {
	if c.circuitBreaker != nil && !c.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("httpclient: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.tenantFromContext {
		if tid := tenant.FromContext(ctx); tid != "" {
			req.Header.Set("X-Tenant-ID", tid)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if c.circuitBreaker != nil {
			c.circuitBreaker.RecordFailure()
		}
		return nil, err
	}
	if c.circuitBreaker != nil {
		if resp.StatusCode >= 500 {
			c.circuitBreaker.RecordFailure()
		} else {
			c.circuitBreaker.RecordSuccess()
		}
	}
	return resp, nil
}

// Do executes a pre-built HTTP request through the configured client.
// Applies timeout, circuit breaker, and configured auth headers (only when the
// caller has not already set them on req). Does NOT retry — request bodies may
// not be replayable. Caller is responsible for closing resp.Body.
// On transport error, resp is nil. On non-2xx, resp.Body is left open for the
// caller (mirrors http.Client.Do).
// Header mutations on req are isolated from the caller via req.Clone.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if c.circuitBreaker != nil && !c.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}
	req = req.Clone(ctx)
	for k, v := range c.headers {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	if c.tenantFromContext {
		if tid := tenant.FromContext(ctx); tid != "" && req.Header.Get("X-Tenant-ID") == "" {
			req.Header.Set("X-Tenant-ID", tid)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if c.circuitBreaker != nil {
			c.circuitBreaker.RecordFailure()
		}
		return nil, err
	}
	if c.circuitBreaker != nil {
		if resp.StatusCode >= 500 {
			c.circuitBreaker.RecordFailure()
		} else {
			c.circuitBreaker.RecordSuccess()
		}
	}
	return resp, nil
}

// NotifyAsync fires a POST in a background goroutine and discards the response.
func (c *Client) NotifyAsync(ctx context.Context, url string, payload interface{}) {
	go func() {
		resp, err := c.PostJSON(ctx, url, payload)
		if err != nil {
			return
		}
		_ = resp.Body.Close()
	}()
}
