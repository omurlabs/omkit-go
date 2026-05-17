package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/omurlabs/omkit-go/provider"
)

func TestOllama_Name(t *testing.T) {
	p := provider.NewOllamaProvider("http://localhost:11434")
	if p.Name() != "ollama" {
		t.Errorf("expected name=ollama, got %q", p.Name())
	}
	if !p.SupportsEmbedding() {
		t.Error("expected SupportsEmbedding=true")
	}
}

func TestOllama_ChatCompletion(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"model": "llama3",
			"message": map[string]any{
				"role":    "assistant",
				"content": "hello world",
			},
			"done_reason":       "stop",
			"eval_count":        42,
			"prompt_eval_count": 10,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := provider.NewOllamaProvider(srv.URL)

	jsonObj := "json_object"
	_ = jsonObj
	req := provider.ChatRequest{
		Model: "llama3",
		Messages: []provider.Message{
			{Role: "user", Content: provider.MessageContent{Text: "hi"}},
		},
		ResponseFormat: &provider.ResponseFormat{Type: "json_object"},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	// Verify path
	if capturedPath != "/api/chat" {
		t.Errorf("expected path /api/chat, got %q", capturedPath)
	}

	// Verify model passed
	if capturedBody["model"] != "llama3" {
		t.Errorf("expected model=llama3, got %v", capturedBody["model"])
	}

	// Verify stream=false
	if capturedBody["stream"] != false {
		t.Errorf("expected stream=false, got %v", capturedBody["stream"])
	}

	// Verify response_format dropped (not present in body)
	if _, ok := capturedBody["format"]; ok {
		t.Error("expected response_format to be dropped, but 'format' key was present")
	}

	// Verify content extracted
	if resp.Content != "hello world" {
		t.Errorf("expected content='hello world', got %q", resp.Content)
	}

	// Verify usage tokens
	if resp.Usage.CompletionTokens != 42 {
		t.Errorf("expected CompletionTokens=42, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected PromptTokens=10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.TotalTokens != 52 {
		t.Errorf("expected TotalTokens=52, got %d", resp.Usage.TotalTokens)
	}
}

func TestOllama_ChatCompletion_Multimodal_TranslatesToNativeFormat(t *testing.T) {
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"model": "medgemma",
			"message": map[string]any{
				"role":    "assistant",
				"content": "an image of a tiny pixel",
			},
			"done_reason":       "stop",
			"eval_count":        5,
			"prompt_eval_count": 3,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := provider.NewOllamaProvider(srv.URL)
	req := provider.ChatRequest{
		Model: "medgemma",
		Messages: []provider.Message{{
			Role: "user",
			Content: provider.MessageContent{Parts: []provider.ContentPart{
				{Type: "text", Text: "describe"},
				{Type: "image_url", ImageURL: &provider.ImageURLObj{URL: "data:image/png;base64,iVBOR=="}},
			}},
		}},
	}

	if _, err := p.ChatCompletion(context.Background(), req); err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	msgs, ok := capturedBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %+v", capturedBody["messages"])
	}
	msg := msgs[0].(map[string]any)
	if got := msg["content"]; got != "describe" {
		t.Errorf("expected content='describe' (text concatenated), got %v", got)
	}
	images, ok := msg["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("expected 1 image, got %+v", msg["images"])
	}
	if images[0] != "iVBOR==" {
		t.Errorf("expected base64-stripped payload, got %v", images[0])
	}
}

func TestOllama_Embedding(t *testing.T) {
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		resp := map[string]any{
			"model": "nomic-embed-text",
			"embeddings": [][]float32{
				{0.1, 0.2, 0.3},
				{0.4, 0.5, 0.6},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := provider.NewOllamaProvider(srv.URL)

	req := provider.EmbedRequest{
		Model: "nomic-embed-text",
		Input: []string{"hello", "world"},
	}

	resp, err := p.Embedding(context.Background(), req)
	if err != nil {
		t.Fatalf("Embedding error: %v", err)
	}

	if capturedPath != "/api/embed" {
		t.Errorf("expected path /api/embed, got %q", capturedPath)
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
}
