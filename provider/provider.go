// provider.go — provider module.
//
// exports: Provider | ContentPart | ImageURLObj | MessageContent | IsMultimodal | String | UnmarshalJSON | MarshalJSON | Message | ResponseFormat | ChatRequest | ChatResponse | EmbedRequest | EmbedResponse | Usage
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package provider

import (
	"context"
	"encoding/json"
	"strings"
)

// Provider is the core abstraction for LLM backends.
type Provider interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Embedding(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
	Name() string
	SupportsEmbedding() bool
}

// ContentPart is a single element of a multimodal message content array,
// matching the OpenAI wire format for vision requests.
type ContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *ImageURLObj `json:"image_url,omitempty"`
}

// ImageURLObj holds the URL (or data-URI) for an image_url content part.
type ImageURLObj struct {
	URL string `json:"url"`
}

// MessageContent is a union type that deserializes from either a JSON string
// or a JSON array of ContentPart objects, matching the OpenAI chat completion
// spec for the "content" field.
//
// Usage:
//
//	// Plain text (backward compat)
//	mc := MessageContent{Text: "hello"}
//
//	// Multimodal parts
//	mc := MessageContent{Parts: []ContentPart{{Type:"text", Text:"hi"},
//	                                           {Type:"image_url", ImageURL:&ImageURLObj{URL:"..."}}}}
type MessageContent struct {
	// Text holds the content when it was a plain JSON string.
	Text string
	// Parts holds the content when it was a JSON array of content parts.
	Parts []ContentPart
}

// IsMultimodal returns true when the content was provided as an array of parts.
func (mc MessageContent) IsMultimodal() bool {
	return len(mc.Parts) > 0
}

// String returns the plain-text representation. For multimodal content it
// concatenates the text parts (images are omitted). Use Parts directly for
// full fidelity.
func (mc MessageContent) String() string {
	if !mc.IsMultimodal() {
		return mc.Text
	}
	var sb strings.Builder
	for _, p := range mc.Parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// UnmarshalJSON implements json.Unmarshaler. It accepts either a JSON string
// or a JSON array of ContentPart objects.
func (mc *MessageContent) UnmarshalJSON(data []byte) error {
	// Trim leading whitespace to detect type.
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) > 0 && trimmed[0] == '"' {
		// Plain string.
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		mc.Text = s
		mc.Parts = nil
		return nil
	}
	// Array of parts.
	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	mc.Parts = parts
	mc.Text = ""
	return nil
}

// MarshalJSON implements json.Marshaler. It emits a plain string when there
// are no parts (Ollama /api/chat string-content path) and an array otherwise.
func (mc MessageContent) MarshalJSON() ([]byte, error) {
	if mc.IsMultimodal() {
		return json.Marshal(mc.Parts)
	}
	return json.Marshal(mc.Text)
}

// Message is an OpenAI-compatible chat message. Content is a union type that
// accepts either a plain string or an array of multimodal content parts.
type Message struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
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
