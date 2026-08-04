package llm

import "fmt"

const defaultDeepSeekBaseURL = "https://api.deepseek.com/v1"

// DeepSeekProvider implements LLMProvider for DeepSeek API (OpenAI-compatible).
//
// DeepSeek API docs: https://api-docs.deepseek.com/
//
// DeepSeekProvider wraps OpenAICompatibleProvider with DeepSeek-specific defaults.
type DeepSeekProvider struct {
	*OpenAICompatibleProvider
}

// NewDeepSeekProvider creates a new DeepSeekProvider.
//
// baseURL defaults to https://api.deepseek.com/v1 when empty.
// model is the model name, e.g. "deepseek-chat" or "deepseek-reasoner".
// apiKey is the Bearer token for authentication.
func NewDeepSeekProvider(baseURL, model, apiKey string) (*DeepSeekProvider, error) {
	if baseURL == "" {
		baseURL = defaultDeepSeekBaseURL
	}
	inner, err := NewOpenAICompatibleProvider(baseURL, model, apiKey)
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	return &DeepSeekProvider{OpenAICompatibleProvider: inner}, nil
}
