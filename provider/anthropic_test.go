package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/provider"
)

func TestAnthropic_ChatCompletion(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"id":    "msg_123",
			"model": "claude-3-5-sonnet-20241022",
			"content": []map[string]any{
				{"type": "text", "text": "hello from claude"},
			},
			"usage": map[string]any{
				"input_tokens":  15,
				"output_tokens": 25,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := provider.NewAnthropicProvider("test-api-key", provider.WithAnthropicBaseURL(srv.URL))

	req := provider.ChatRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []provider.Message{
			{Role: "user", Content: provider.MessageContent{Text: "hi"}},
		},
		ResponseFormat: &provider.ResponseFormat{Type: "json_object"},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	// Verify headers
	if capturedHeaders.Get("x-api-key") != "test-api-key" {
		t.Errorf("expected x-api-key=test-api-key, got %q", capturedHeaders.Get("x-api-key"))
	}
	if capturedHeaders.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("expected anthropic-version=2023-06-01, got %q", capturedHeaders.Get("anthropic-version"))
	}
	if capturedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", capturedHeaders.Get("Content-Type"))
	}

	// Verify model passed
	if capturedBody["model"] != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected model=claude-3-5-sonnet-20241022, got %v", capturedBody["model"])
	}

	// Verify ResponseFormat dropped (not present in body)
	if _, ok := capturedBody["response_format"]; ok {
		t.Error("expected response_format to be dropped for Anthropic")
	}

	// Verify content extracted
	if resp.Content != "hello from claude" {
		t.Errorf("expected content='hello from claude', got %q", resp.Content)
	}

	// Verify usage
	if resp.Usage.PromptTokens != 15 {
		t.Errorf("expected PromptTokens=15, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 25 {
		t.Errorf("expected CompletionTokens=25, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 40 {
		t.Errorf("expected TotalTokens=40, got %d", resp.Usage.TotalTokens)
	}
}

func TestAnthropic_NoEmbedding(t *testing.T) {
	p := provider.NewAnthropicProvider("test-api-key")

	if p.SupportsEmbedding() {
		t.Error("expected SupportsEmbedding=false for Anthropic")
	}

	_, err := p.Embedding(context.Background(), provider.EmbedRequest{
		Model: "any-model",
		Input: []string{"hello"},
	})
	if err == nil {
		t.Fatal("expected error from Embedding, got nil")
	}
	if err.Error() != "anthropic does not support embeddings" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}
