// client.go — client module.
//
// exports: ErrCircuitOpen | ErrRemoteFailure | ErrDisabled | Client | Option | WithTimeout | WithEnabled | WithFailureThreshold | WithCooldown | New | Available | NER | Classify | DetectLanguage | Translate | Embed | Health
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package cerebellum provides an HTTP client for the Cerebellum biomedical NLP service.
package cerebellum

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const maxBatchSize = 32

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("cerebellum: circuit open")

// ErrRemoteFailure is returned when the remote call failed (transport, 5xx,
// decode). Callers may treat this the same as "unavailable" and fall back.
var ErrRemoteFailure = errors.New("cerebellum: remote failure")

// ErrDisabled is returned when the client was constructed with WithEnabled(false).
var ErrDisabled = errors.New("cerebellum: disabled")

// Client is an HTTP client for Cerebellum with circuit breaker.
type Client struct {
	baseURL          string
	timeout          time.Duration
	failureThreshold int
	cooldownSeconds  float64
	enabled          bool

	mu              sync.Mutex
	failures        int
	circuitOpenedAt time.Time
	circuitOpen     bool
	client          *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout sets the HTTP timeout.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithEnabled sets whether the client is enabled.
func WithEnabled(enabled bool) Option { return func(c *Client) { c.enabled = enabled } }

// WithFailureThreshold overrides the default consecutive-failure threshold
// before the breaker trips (default 5).
func WithFailureThreshold(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.failureThreshold = n
		}
	}
}

// WithCooldown overrides the default half-open cooldown in seconds.
func WithCooldown(seconds float64) Option {
	return func(c *Client) {
		if seconds > 0 {
			c.cooldownSeconds = seconds
		}
	}
}

// New creates a Cerebellum client. The underlying transport is wrapped with
// otelhttp so outbound requests propagate W3C trace context.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:          baseURL,
		timeout:          5 * time.Second,
		failureThreshold: 5,
		cooldownSeconds:  60.0,
		enabled:          true,
	}
	for _, o := range opts {
		o(c)
	}
	c.client = &http.Client{
		Timeout:   c.timeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	return c
}

// Available returns false when disabled or circuit is open.
func (c *Client) Available() bool {
	if !c.enabled {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.circuitOpen {
		return true
	}
	elapsed := time.Since(c.circuitOpenedAt).Seconds()
	return elapsed >= c.cooldownSeconds
}

func (c *Client) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.circuitOpen = false
}

func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= c.failureThreshold && !c.circuitOpen {
		c.circuitOpen = true
		c.circuitOpenedAt = time.Now()
		slog.Warn("cerebellum.circuit_opened", "failures", c.failures)
	}
}

// post executes a JSON POST. Returns ErrDisabled when the client is disabled,
// ErrCircuitOpen when the breaker is open, or ErrRemoteFailure (wrapped) on
// transport/status/decode errors. On success, returns the decoded JSON object.
func (c *Client) post(ctx context.Context, endpoint string, payload any, requestID, tenantID string) (map[string]any, error) {
	if !c.enabled {
		return nil, ErrDisabled
	}
	if !c.Available() {
		return nil, ErrCircuitOpen
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cerebellum: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cerebellum: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.recordFailure()
		slog.Warn("cerebellum.request_failed", "endpoint", endpoint, "error", err)
		return nil, fmt.Errorf("%w: %v", ErrRemoteFailure, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		c.recordFailure()
		slog.Warn("cerebellum.request_failed", "endpoint", endpoint, "status", resp.StatusCode)
		return nil, fmt.Errorf("%w: status %d", ErrRemoteFailure, resp.StatusCode)
	}

	c.recordSuccess()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrRemoteFailure, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrRemoteFailure, err)
	}
	return result, nil
}

func splitBatch[T any](items []T) [][]T {
	var batches [][]T
	for i := 0; i < len(items); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

// NER runs named entity recognition. Returns ErrRemoteFailure / ErrCircuitOpen
// on failure so callers can choose to degrade vs surface.
func (c *Client) NER(ctx context.Context, texts []string, requestID, tenantID string) ([]map[string]any, error) {
	var all []map[string]any
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/ner", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if results, ok := result["results"].([]any); ok {
			for _, r := range results {
				if m, ok := r.(map[string]any); ok {
					all = append(all, m)
				}
			}
		}
	}
	return all, nil
}

// Classify classifies texts.
func (c *Client) Classify(ctx context.Context, texts []string, requestID, tenantID string) ([]map[string]any, error) {
	var all []map[string]any
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/classify", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if results, ok := result["results"].([]any); ok {
			for _, r := range results {
				if m, ok := r.(map[string]any); ok {
					all = append(all, m)
				}
			}
		}
	}
	return all, nil
}

// DetectLanguage detects language of texts.
func (c *Client) DetectLanguage(ctx context.Context, texts []string, requestID, tenantID string) ([]map[string]any, error) {
	var all []map[string]any
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/detect-language", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if results, ok := result["results"].([]any); ok {
			for _, r := range results {
				if m, ok := r.(map[string]any); ok {
					all = append(all, m)
				}
			}
		}
	}
	return all, nil
}

// Translate translates texts to English.
func (c *Client) Translate(ctx context.Context, texts []string, sourceLang string, requestID, tenantID string) ([]map[string]any, error) {
	var all []map[string]any
	for _, batch := range splitBatch(texts) {
		payload := map[string]any{"texts": batch}
		if sourceLang != "" {
			payload["source_lang"] = sourceLang
		}
		result, err := c.post(ctx, "/translate", payload, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if results, ok := result["translations"].([]any); ok {
			for _, r := range results {
				if m, ok := r.(map[string]any); ok {
					all = append(all, m)
				}
			}
		}
	}
	return all, nil
}

// Embed gets embeddings for texts.
func (c *Client) Embed(ctx context.Context, texts []string, requestID, tenantID string) ([][]float64, error) {
	var all [][]float64
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/embed", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if embeddings, ok := result["embeddings"].([]any); ok {
			for _, e := range embeddings {
				if arr, ok := e.([]any); ok {
					vec := make([]float64, len(arr))
					for i, v := range arr {
						if f, ok := v.(float64); ok {
							vec[i] = f
						}
					}
					all = append(all, vec)
				}
			}
		}
	}
	return all, nil
}

// Health checks if Cerebellum is reachable.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("cerebellum unhealthy: %d", resp.StatusCode)
	}
	return nil
}
