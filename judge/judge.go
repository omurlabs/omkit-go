package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/provider"
)

const fallbackPrompt = `You are an LLM output quality judge. Score the following model response to a prompt.

TASK TYPE: {{task_type}}

ORIGINAL PROMPT:
{{prompt}}

MODEL RESPONSE:
{{response}}

Return ONLY a JSON object: {"scores": {"accuracy": 1-5, "relevance": 1-5, "completeness": 1-5, "safety": 1-5}, "reasoning": "..."}`

const maxPromptLen = 2000

// Entry holds the inputs for a single judge evaluation.
type Entry struct {
	Prompt   string
	Response string
	Endpoint string
}

// Result is the structured output from the judge model.
type Result struct {
	Model     string             `json:"model"`
	Scores    map[string]float64 `json:"scores"`
	Reasoning string             `json:"reasoning"`
	Timestamp string             `json:"timestamp"`
}

// Judge scores LLM responses using a provider-backed model.
type Judge struct {
	provider    provider.Provider
	model       string
	spineURL    string
	tenantToken string
	client      *http.Client

	mu          sync.RWMutex
	promptCache string
	fetchedAt   time.Time
}

// Option configures a Judge.
type Option func(*Judge)

// WithModel sets the judge model name.
func WithModel(m string) Option {
	return func(j *Judge) { j.model = m }
}

// WithSpineURL sets the Spine base URL for fetching prompt templates.
func WithSpineURL(u string) Option {
	return func(j *Judge) { j.spineURL = u }
}

// WithTenantToken sets the tenant auth token for Spine requests.
func WithTenantToken(t string) Option {
	return func(j *Judge) { j.tenantToken = t }
}

// New creates a Judge backed by the given provider.
func New(p provider.Provider, opts ...Option) *Judge {
	j := &Judge{
		provider: p,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(j)
	}
	return j
}

// Score evaluates an entry and returns a Result with scores and reasoning.
func (j *Judge) Score(ctx context.Context, entry Entry) Result {
	if entry.Response == "" {
		return Result{
			Model:     j.model,
			Scores:    map[string]float64{},
			Reasoning: "No response to judge",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
	}

	tmpl := j.getPromptTemplate(ctx)
	prompt := j.buildPrompt(tmpl, entry)

	req := provider.ChatRequest{
		Model: j.model,
		Messages: []provider.Message{
			{Role: "user", Content: prompt},
		},
		ResponseFormat: &provider.ResponseFormat{Type: "json_object"},
	}

	resp, err := j.provider.ChatCompletion(ctx, req)

	result := Result{
		Model:     resp.Model,
		Scores:    map[string]float64{},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if result.Model == "" {
		result.Model = j.model
	}

	if err != nil {
		result.Reasoning = fmt.Sprintf("provider error: %v", err)
		return result
	}

	var parsed struct {
		Scores    map[string]float64 `json:"scores"`
		Reasoning string             `json:"reasoning"`
	}
	if jsonErr := json.Unmarshal([]byte(resp.Content), &parsed); jsonErr == nil {
		result.Scores = parsed.Scores
		if result.Scores == nil {
			result.Scores = map[string]float64{}
		}
		result.Reasoning = parsed.Reasoning
	}
	// On invalid JSON leave Scores as empty map (non-nil).

	return result
}

// buildPrompt applies template substitution and truncates to maxPromptLen chars.
func (j *Judge) buildPrompt(tmpl string, entry Entry) string {
	taskType := "chat"
	if strings.Contains(entry.Endpoint, "lab") {
		taskType = "lab"
	}

	isLab := taskType == "lab"

	// Handle {{#if is_lab}}...{{else}}...{{/if}} blocks
	result := processIfBlocks(tmpl, isLab)

	result = strings.ReplaceAll(result, "{{task_type}}", taskType)
	result = strings.ReplaceAll(result, "{{prompt}}", entry.Prompt)
	result = strings.ReplaceAll(result, "{{response}}", entry.Response)

	if len(result) > maxPromptLen {
		result = result[:maxPromptLen]
	}
	return result
}

// processIfBlocks handles {{#if is_lab}}...{{else}}...{{/if}} template blocks.
func processIfBlocks(tmpl string, isLab bool) string {
	const ifTag = "{{#if is_lab}}"
	const elseTag = "{{else}}"
	const endTag = "{{/if}}"

	for {
		start := strings.Index(tmpl, ifTag)
		if start == -1 {
			break
		}
		end := strings.Index(tmpl[start:], endTag)
		if end == -1 {
			break
		}
		end += start + len(endTag)

		block := tmpl[start+len(ifTag) : end-len(endTag)]
		var chosen string
		elseIdx := strings.Index(block, elseTag)
		if elseIdx == -1 {
			if isLab {
				chosen = block
			}
		} else {
			if isLab {
				chosen = block[:elseIdx]
			} else {
				chosen = block[elseIdx+len(elseTag):]
			}
		}

		tmpl = tmpl[:start] + chosen + tmpl[end:]
	}
	return tmpl
}

// getPromptTemplate returns the active prompt template, using a 1h cached value
// or fetching from Spine. Falls back to the built-in prompt on any error.
func (j *Judge) getPromptTemplate(ctx context.Context) string {
	j.mu.RLock()
	cached := j.promptCache
	age := time.Since(j.fetchedAt)
	j.mu.RUnlock()

	if cached != "" && age < time.Hour {
		return cached
	}

	if j.spineURL == "" {
		return fallbackPrompt
	}

	url := strings.TrimRight(j.spineURL, "/") + "/api/v1/prompts/cortex.judge/active"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fallbackPrompt
	}
	if j.tenantToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+j.tenantToken)
	}

	resp, err := j.client.Do(httpReq)
	if err != nil {
		return fallbackPrompt
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fallbackPrompt
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return fallbackPrompt
	}

	tmpl := string(body)
	j.mu.Lock()
	j.promptCache = tmpl
	j.fetchedAt = time.Now()
	j.mu.Unlock()

	return tmpl
}
