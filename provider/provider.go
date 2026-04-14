package provider

import "context"

// Provider is the core abstraction for LLM backends.
type Provider interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Embedding(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
	Name() string
	SupportsEmbedding() bool
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponseFormat struct {
	Type string `json:"type"` // "json_object" or "text"
}

type ChatRequest struct {
	Model          string
	Messages       []Message
	Temperature    *float64
	MaxTokens      *int
	ResponseFormat *ResponseFormat
}

type ChatResponse struct {
	Content      string
	Model        string
	Usage        Usage
	FinishReason string
}

type EmbedRequest struct {
	Model string
	Input []string
}

type EmbedResponse struct {
	Embeddings [][]float32
	Model      string
	Usage      Usage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}
