// anthropic.go — anthropic module.
//
// exports: AnthropicProvider | AnthropicOption | WithAnthropicBaseURL | NewAnthropicProvider | Name | SupportsEmbedding | Embedding | ChatCompletion
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const defaultAnthropicBaseURL = "https://api.anthropic.com"
const defaultAnthropicVersion = "2023-06-01"
const defaultMaxTokens = 4096

// AnthropicProvider implements Provider for the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// AnthropicOption configures an AnthropicProvider.
type AnthropicOption func(*AnthropicProvider)

// WithAnthropicBaseURL overrides the default Anthropic API base URL.
func WithAnthropicBaseURL(url string) AnthropicOption {
	return func(p *AnthropicProvider) {
		p.baseURL = url
	}
}

// NewAnthropicProvider creates a new AnthropicProvider with the given API key.
func NewAnthropicProvider(apiKey string, opts ...AnthropicOption) *AnthropicProvider {
	p := &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: defaultAnthropicBaseURL,
		client:  &http.Client{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *AnthropicProvider) Name() string            { return "anthropic" }
func (p *AnthropicProvider) SupportsEmbedding() bool { return false }

// Embedding is unsupported for Anthropic.
func (p *AnthropicProvider) Embedding(_ context.Context, _ EmbedRequest) (EmbedResponse, error) {
	return EmbedResponse{}, errors.New("anthropic does not support embeddings")
}

type anthropicChatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Temperature *float64 `json:"temperature,omitempty"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicChatResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Content []anthropicContent `json:"content"`
	Usage   anthropicUsage     `json:"usage"`
}

// ChatCompletion sends a request to the Anthropic Messages API.
// ResponseFormat is dropped (unsupported by Anthropic).
func (p *AnthropicProvider) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	maxTokens := defaultMaxTokens
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	body := anthropicChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	}
	// ResponseFormat intentionally dropped — Anthropic does not support it.

	data, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", defaultAnthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}

	var anthropicResp anthropicChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: decode response: %w", err)
	}

	content := ""
	if len(anthropicResp.Content) > 0 {
		content = anthropicResp.Content[0].Text
	}

	usage := Usage{
		PromptTokens:     anthropicResp.Usage.InputTokens,
		CompletionTokens: anthropicResp.Usage.OutputTokens,
		TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
	}

	return ChatResponse{
		Content: content,
		Model:   anthropicResp.Model,
		Usage:   usage,
	}, nil
}
