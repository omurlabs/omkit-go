package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OllamaProvider implements Provider for local Ollama instances.
type OllamaProvider struct {
	baseURL string
	client  *http.Client
}

// NewOllamaProvider creates a new OllamaProvider targeting baseURL.
func NewOllamaProvider(baseURL string) *OllamaProvider {
	return &OllamaProvider{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (p *OllamaProvider) Name() string           { return "ollama" }
func (p *OllamaProvider) SupportsEmbedding() bool { return true }

// ollamaChatRequest is the wire format for /api/chat.
type ollamaChatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Model   string  `json:"model"`
	Message Message `json:"message"`
	// Done fields
	DoneReason      string `json:"done_reason"`
	EvalCount       int    `json:"eval_count"`
	PromptEvalCount int    `json:"prompt_eval_count"`
}

// ChatCompletion sends a request to Ollama /api/chat.
// ResponseFormat is dropped (unsupported by Ollama).
func (p *OllamaProvider) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body := ollamaChatRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   false,
	}

	if req.Temperature != nil {
		body.Options = map[string]any{
			"temperature": *req.Temperature,
		}
	}

	// ResponseFormat intentionally dropped — Ollama does not support it.

	data, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("ollama: unexpected status %d", resp.StatusCode)
	}

	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return ChatResponse{}, fmt.Errorf("ollama: decode response: %w", err)
	}

	usage := Usage{
		PromptTokens:     ollamaResp.PromptEvalCount,
		CompletionTokens: ollamaResp.EvalCount,
		TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
	}

	return ChatResponse{
		Content:      ollamaResp.Message.Content,
		Model:        ollamaResp.Model,
		Usage:        usage,
		FinishReason: ollamaResp.DoneReason,
	}, nil
}

// ollamaEmbedRequest is the wire format for /api/embed.
type ollamaEmbedRequest struct {
	Model  string   `json:"model"`
	Input  []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// Embedding sends a request to Ollama /api/embed.
func (p *OllamaProvider) Embedding(ctx context.Context, req EmbedRequest) (EmbedResponse, error) {
	body := ollamaEmbedRequest{
		Model: req.Model,
		Input: req.Input,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return EmbedResponse{}, fmt.Errorf("ollama: marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/embed", bytes.NewReader(data))
	if err != nil {
		return EmbedResponse{}, fmt.Errorf("ollama: build embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return EmbedResponse{}, fmt.Errorf("ollama: do embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return EmbedResponse{}, fmt.Errorf("ollama: unexpected status %d", resp.StatusCode)
	}

	var ollamaResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return EmbedResponse{}, fmt.Errorf("ollama: decode embed response: %w", err)
	}

	return EmbedResponse{
		Embeddings: ollamaResp.Embeddings,
		Model:      ollamaResp.Model,
	}, nil
}
