// Package cerebellum provides an HTTP client for the Cerebellum biomedical NLP service.
package cerebellum

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const maxBatchSize = 32

// Client is an HTTP client for Cerebellum with circuit breaker.
type Client struct {
	baseURL          string
	timeout          time.Duration
	failureThreshold int
	cooldownSeconds  float64
	enabled          bool

	mu               sync.Mutex
	failures         int
	circuitOpenedAt  time.Time
	circuitOpen      bool
	client           *http.Client
}

// New creates a Cerebellum client.
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
	c.client = &http.Client{Timeout: c.timeout}
	return c
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout sets the HTTP timeout.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithEnabled sets whether the client is enabled.
func WithEnabled(enabled bool) Option { return func(c *Client) { c.enabled = enabled } }

// Available returns false when disabled or circuit is open.
func (c *Client) Available() bool {
	if !c.enabled {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failures < c.failureThreshold {
		return true
	}
	if c.circuitOpen {
		elapsed := time.Since(c.circuitOpenedAt).Seconds()
		if elapsed >= c.cooldownSeconds {
			return true // half-open
		}
	}
	return false
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

func (c *Client) post(ctx context.Context, endpoint string, payload any, requestID, tenantID string) (map[string]any, error) {
	if !c.Available() {
		return nil, nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
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
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 503 {
		c.recordFailure()
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		c.recordFailure()
		slog.Warn("cerebellum.request_failed", "endpoint", endpoint, "status", resp.StatusCode)
		return nil, nil
	}

	c.recordSuccess()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, nil
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

// NER runs named entity recognition.
func (c *Client) NER(ctx context.Context, texts []string, requestID, tenantID string) ([]map[string]any, error) {
	if !c.Available() {
		return nil, nil
	}
	var all []map[string]any
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/ner", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
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
	if !c.Available() {
		return nil, nil
	}
	var all []map[string]any
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/classify", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
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
	if !c.Available() {
		return nil, nil
	}
	var all []map[string]any
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/detect-language", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
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
	if !c.Available() {
		return nil, nil
	}
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
		if result == nil {
			return nil, nil
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
	if !c.Available() {
		return nil, nil
	}
	var all [][]float64
	for _, batch := range splitBatch(texts) {
		result, err := c.post(ctx, "/embed", map[string]any{"texts": batch}, requestID, tenantID)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
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
