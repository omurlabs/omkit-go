package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omurlabs/omkit-go/provider"
)

func TestOpenAI_ChatCompletion(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "gpt-4o",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello from openai",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     12,
				"completion_tokens": 20,
				"total_tokens":      32,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := provider.NewOpenAIProvider("test-openai-key", provider.WithOpenAIBaseURL(srv.URL))

	req := provider.ChatRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: provider.MessageContent{Text: "hi"}},
		},
		ResponseFormat: &provider.ResponseFormat{Type: "json_object"},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	// Verify auth header
	if capturedHeaders.Get("Authorization") != "Bearer test-openai-key" {
		t.Errorf("expected Authorization=Bearer test-openai-key, got %q", capturedHeaders.Get("Authorization"))
	}

	// Verify response_format passed through
	if capturedBody["response_format"] == nil {
		t.Error("expected response_format to be passed through for OpenAI")
	}

	// Verify content extracted
	if resp.Content != "hello from openai" {
		t.Errorf("expected content='hello from openai', got %q", resp.Content)
	}

	// Verify finish_reason
	if resp.FinishReason != "stop" {
		t.Errorf("expected FinishReason=stop, got %q", resp.FinishReason)
	}

	// Verify usage
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("expected PromptTokens=12, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 20 {
		t.Errorf("expected CompletionTokens=20, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 32 {
		t.Errorf("expected TotalTokens=32, got %d", resp.Usage.TotalTokens)
	}
}

func TestOpenAI_Embedding(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"model": "text-embedding-3-small",
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}, "index": 0},
				{"embedding": []float64{0.4, 0.5, 0.6}, "index": 1},
			},
			"usage": map[string]any{
				"prompt_tokens": 4,
				"total_tokens":  4,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := provider.NewOpenAIProvider("test-openai-key", provider.WithOpenAIBaseURL(srv.URL))

	req := provider.EmbedRequest{
		Model: "text-embedding-3-small",
		Input: []string{"hello", "world"},
	}

	resp, err := p.Embedding(context.Background(), req)
	if err != nil {
		t.Fatalf("Embedding error: %v", err)
	}

	if capturedPath != "/v1/embeddings" {
		t.Errorf("expected path /v1/embeddings, got %q", capturedPath)
	}

	if capturedBody["model"] != "text-embedding-3-small" {
		t.Errorf("expected model=text-embedding-3-small, got %v", capturedBody["model"])
	}

	if len(resp.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0]) != 3 {
		t.Errorf("expected embedding dim=3, got %d", len(resp.Embeddings[0]))
	}
	if resp.Embeddings[0][0] != 0.1 {
		t.Errorf("expected first value=0.1, got %v", resp.Embeddings[0][0])
	}

	if resp.Usage.PromptTokens != 4 {
		t.Errorf("expected PromptTokens=4, got %d", resp.Usage.PromptTokens)
	}
}
