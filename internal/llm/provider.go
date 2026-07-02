// Package llm provides LLM provider abstractions for inference and embedding.
package llm

import "context"

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`    // "system", "user", or "assistant"
	Content string `json:"content"` // message body
}

// ChatOptions holds optional parameters for a chat request.
type ChatOptions struct {
	// Temperature controls randomness (0-1). 0 means deterministic.
	Temperature float64

	// MaxTokens limits the maximum number of tokens in the response.
	// 0 means use the provider default.
	MaxTokens int

	// Model overrides the default model for this request.
	// Empty means use the configured default.
	Model string
}

// ChatResponse holds a complete (non-streaming) chat response.
type ChatResponse struct {
	Content string
	Usage   TokenUsage
}

// Chunk represents a single streaming response fragment.
type Chunk struct {
	Content string // delta text
	Done    bool   // true if this is the last chunk
	Error   error  // non-nil on stream error
}

// TokenUsage reports token counts for a request.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
}

// LLMProvider is the interface for LLM inference backends.
type LLMProvider interface {
	// Chat sends a conversation and returns the complete response.
	Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error)

	// ChatStream sends a conversation and returns a channel of streaming chunks.
	// The channel is closed when streaming completes or an error occurs.
	ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan Chunk, error)
}

// NewProvider creates an LLM provider based on the config.
// Supported providers: "ollama".
func NewProvider(provider, baseURL, model string) (LLMProvider, error) {
	switch provider {
	case "ollama":
		return NewOllamaProvider(baseURL, model), nil
	default:
		return nil, &ErrUnsupportedProvider{Provider: provider}
	}
}

// ErrUnsupportedProvider is returned when the provider name is unknown.
type ErrUnsupportedProvider struct {
	Provider string
}

func (e *ErrUnsupportedProvider) Error() string {
	return "unsupported LLM provider: " + e.Provider
}
