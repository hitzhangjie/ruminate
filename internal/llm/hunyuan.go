package llm

import "fmt"

const defaultHunyuanBaseURL = "http://api.taiji.woa.com/openapi/v2"

// HunyuanProvider implements LLMProvider for Tencent Hunyuan API (OpenAI-compatible).
//
// Hunyuan API docs: the Chat Completions API endpoint is at
//
//	POST /openapi/v2/chat/completions
//
// with base URL http://api.taiji.woa.com.
// It uses Bearer token auth and is largely OpenAI-compatible.
//
// HunyuanProvider wraps OpenAICompatibleProvider with Hunyuan-specific defaults.
type HunyuanProvider struct {
	*OpenAICompatibleProvider
}

// NewHunyuanProvider creates a new HunyuanProvider.
//
// baseURL defaults to http://api.taiji.woa.com/openapi/v2 when empty.
// model is the model name, typically "hunyuan".
// apiKey is the Bearer token for authentication.
func NewHunyuanProvider(baseURL, model, apiKey string) (*HunyuanProvider, error) {
	if baseURL == "" {
		baseURL = defaultHunyuanBaseURL
	}
	inner, err := NewOpenAICompatibleProvider(baseURL, model, apiKey)
	if err != nil {
		return nil, fmt.Errorf("hunyuan: %w", err)
	}
	return &HunyuanProvider{OpenAICompatibleProvider: inner}, nil
}
