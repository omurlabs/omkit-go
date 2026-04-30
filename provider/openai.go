// openai.go — openai module.
//
// exports: OpenAIProvider | OpenAIOption | WithOpenAIBaseURL | NewOpenAIProvider | Name | SupportsEmbedding | ChatCompletion | Embedding
// used_by: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultOpenAIBaseURL = "https://api.openai.com"

// OpenAIProvider implements Provider for the OpenAI API.
type OpenAIProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// OpenAIOption configures an OpenAIProvider.
type OpenAIOption func(*OpenAIProvider)

// WithOpenAIBaseURL overrides the default OpenAI API base URL.
func WithOpenAIBaseURL(url string) OpenAIOption {
	return func(p *OpenAIProvider) {
		p.baseURL = url
	}
}

// NewOpenAIProvider creates a new OpenAIProvider with the given API key.
func NewOpenAIProvider(apiKey string, opts ...OpenAIOption) *OpenAIProvider {
	p := &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: defaultOpenAIBaseURL,
		client:  &http.Client{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *OpenAIProvider) Name() string            { return "openai" }
func (p *OpenAIProvider) SupportsEmbedding() bool { return true }

type openaiChatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiChoice struct {
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openaiChatResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
}

// ChatCompletion sends a request to the OpenAI chat completions API.
// ResponseFormat is passed through (OpenAI supports it).
func (p *OpenAIProvider) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body := openaiChatRequest{
		Model:          req.Model,
		Messages:       req.Messages,
		Temperature:    req.Temperature,
		MaxTokens:      req.MaxTokens,
		ResponseFormat: req.ResponseFormat,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("openai: unexpected status %d", resp.StatusCode)
	}

	var openaiResp openaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return ChatResponse{}, fmt.Errorf("openai: decode response: %w", err)
	}

	content := ""
	finishReason := ""
	if len(openaiResp.Choices) > 0 {
		content = openaiResp.Choices[0].Message.Content
		finishReason = openaiResp.Choices[0].FinishReason
	}

	usage := Usage{
		PromptTokens:     openaiResp.Usage.PromptTokens,
		CompletionTokens: openaiResp.Usage.CompletionTokens,
		TotalTokens:      openaiResp.Usage.TotalTokens,
	}

	return ChatResponse{
		Content:      content,
		Model:        openaiResp.Model,
		Usage:        usage,
		FinishReason: finishReason,
	}, nil
}

type openaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type openaiEmbedUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type openaiEmbedResponse struct {
	Model string            `json:"model"`
	Data  []openaiEmbedData `json:"data"`
	Usage openaiEmbedUsage  `json:"usage"`
}

// Embedding sends a request to the OpenAI embeddings API.
func (p *OpenAIProvider) Embedding(ctx context.Context, req EmbedRequest) (EmbedResponse, error) {
	body := openaiEmbedRequest{
		Model: req.Model,
		Input: req.Input,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return EmbedResponse{}, fmt.Errorf("openai: marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/embeddings", bytes.NewReader(data))
	if err != nil {
		return EmbedResponse{}, fmt.Errorf("openai: build embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return EmbedResponse{}, fmt.Errorf("openai: do embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return EmbedResponse{}, fmt.Errorf("openai: unexpected status %d", resp.StatusCode)
	}

	var openaiResp openaiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return EmbedResponse{}, fmt.Errorf("openai: decode embed response: %w", err)
	}

	embeddings := make([][]float32, len(openaiResp.Data))
	for i, d := range openaiResp.Data {
		embeddings[i] = d.Embedding
	}

	usage := Usage{
		PromptTokens: openaiResp.Usage.PromptTokens,
		TotalTokens:  openaiResp.Usage.TotalTokens,
	}

	return EmbedResponse{
		Embeddings: embeddings,
		Model:      openaiResp.Model,
		Usage:      usage,
	}, nil
}
