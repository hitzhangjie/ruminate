package llm

import (
	"fmt"
	"strings"
)

const defaultOllamaBaseURL = "http://localhost:11434"

// OllamaProvider implements LLMProvider for Ollama (OpenAI-compatible).
//
// Ollama exposes OpenAI-compatible endpoints under /v1/ (see
// https://docs.ollama.com/api/openai-compatibility), so OllamaProvider
// wraps OpenAICompatibleProvider with Ollama-specific defaults —
// the same pattern used by DeepSeekProvider and HunyuanProvider.
type OllamaProvider struct {
	*OpenAICompatibleProvider
}

// NewOllamaProvider creates a new OllamaProvider.
//
// baseURL is the Ollama API endpoint (e.g. "http://localhost:11434").
// If empty, defaults to http://localhost:11434.
// model is the default model name (e.g. "llama3.2").
//
// Ollama ignores the Authorization header, so we pass a dummy apiKey.
func NewOllamaProvider(baseURL, model string) (*OllamaProvider, error) {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Ollama's OpenAI-compatible endpoints are served under /v1.
	// Ollama requires an api_key but ignores its value.
	inner, err := NewOpenAICompatibleProvider(baseURL+"/v1", model, "ollama")
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	return &OllamaProvider{OpenAICompatibleProvider: inner}, nil
}
