package judge_test

import (
	"context"
	"testing"

	"github.com/omurlabs/omkit-go/judge"
	"github.com/omurlabs/omkit-go/provider"
)

type mockProvider struct {
	response string
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) SupportsEmbedding() bool { return false }
func (m *mockProvider) Embedding(_ context.Context, _ provider.EmbedRequest) (provider.EmbedResponse, error) {
	return provider.EmbedResponse{}, nil
}
func (m *mockProvider) ChatCompletion(_ context.Context, _ provider.ChatRequest) (provider.ChatResponse, error) {
	return provider.ChatResponse{Content: m.response}, nil
}

func TestScore_ValidJSON(t *testing.T) {
	mock := &mockProvider{
		response: `{"scores":{"accuracy":4,"relevance":5},"reasoning":"good"}`,
	}
	j := judge.New(mock)
	result := j.Score(context.Background(), judge.Entry{
		Prompt:   "What is 2+2?",
		Response: "4",
		Endpoint: "/api/chat",
	})

	if result.Reasoning != "good" {
		t.Errorf("expected reasoning 'good', got %q", result.Reasoning)
	}
	if result.Scores["accuracy"] != 4 {
		t.Errorf("expected accuracy=4, got %v", result.Scores["accuracy"])
	}
	if result.Scores["relevance"] != 5 {
		t.Errorf("expected relevance=5, got %v", result.Scores["relevance"])
	}
}

func TestScore_EmptyResponse(t *testing.T) {
	mock := &mockProvider{response: ""}
	j := judge.New(mock)
	result := j.Score(context.Background(), judge.Entry{
		Prompt:   "hello",
		Response: "",
		Endpoint: "/api/chat",
	})

	if result.Reasoning != "No response to judge" {
		t.Errorf("expected 'No response to judge', got %q", result.Reasoning)
	}
}

func TestScore_InvalidJSON(t *testing.T) {
	mock := &mockProvider{response: "not json"}
	j := judge.New(mock)
	result := j.Score(context.Background(), judge.Entry{
		Prompt:   "hello",
		Response: "world",
		Endpoint: "/api/chat",
	})

	if result.Scores == nil {
		t.Error("expected non-nil scores map on invalid JSON")
	}
}

func TestBuildPrompt_ChatEndpoint(t *testing.T) {
	mock := &mockProvider{response: `{"scores":{},"reasoning":"ok"}`}
	j := judge.New(mock)
	// Score indirectly exercises buildPrompt; we verify it doesn't return empty reasoning
	result := j.Score(context.Background(), judge.Entry{
		Prompt:   "Tell me a joke",
		Response: "Why did the chicken cross the road? To get to the other side.",
		Endpoint: "/api/chat",
	})

	// The prompt was built and sent — if we get here without panic the prompt was non-empty
	if result.Reasoning == "" {
		t.Error("expected non-empty reasoning")
	}
}
