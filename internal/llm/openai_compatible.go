package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

// OpenAICompatibleProvider implements LLMProvider for any OpenAI-compatible API.
//
// It uses the openai-go SDK with configurable baseURL and model, making it
// suitable for services like OpenAI, DeepSeek, Hunyuan, and other compatible
// providers.
//
// This also serves as the base provider embedded by HunyuanProvider and
// DeepSeekProvider for provider-specific defaults.
type OpenAICompatibleProvider struct {
	baseURL string
	model   string
	client  openai.Client
}

// NewOpenAICompatibleProvider creates a new OpenAICompatibleProvider.
//
// baseURL is the API base URL (e.g. "https://api.openai.com/v1").
// When empty, defaults to the openai-go SDK default (https://api.openai.com/v1).
// model is the default model name.
// apiKey is the Bearer token for authentication. Required.
func NewOpenAICompatibleProvider(baseURL, model, apiKey string) (*OpenAICompatibleProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openai: api key is required")
	}

	var opts []option.RequestOption
	opts = append(opts, option.WithAPIKey(apiKey))
	if baseURL != "" {
		baseURL = strings.TrimSuffix(baseURL, "/")
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)

	return &OpenAICompatibleProvider{
		baseURL: baseURL,
		model:   model,
		client:  client,
	}, nil
}

// Chat sends a non-streaming chat request.
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error) {
	oaiMessages := p.convertMessages(messages)

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(p.resolveModel(opts)),
		Messages: oaiMessages,
	}

	if opts != nil {
		if opts.Temperature > 0 {
			params.Temperature = param.NewOpt(opts.Temperature)
		}
		if opts.MaxTokens > 0 {
			params.MaxCompletionTokens = param.NewOpt(int64(opts.MaxTokens))
		}
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}

	return p.parseResponse(resp), nil
}

// ChatStream sends a streaming chat request.
func (p *OpenAICompatibleProvider) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan Chunk, error) {
	oaiMessages := p.convertMessages(messages)

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(p.resolveModel(opts)),
		Messages: oaiMessages,
	}

	if opts != nil {
		if opts.Temperature > 0 {
			params.Temperature = param.NewOpt(opts.Temperature)
		}
		if opts.MaxTokens > 0 {
			params.MaxCompletionTokens = param.NewOpt(int64(opts.MaxTokens))
		}
	}

	// Enable usage chunk in streaming (some providers return usage in a final
	// chunk when stream_options.include_usage is true).
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: param.NewOpt(true),
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan Chunk, 10)
	go func() {
		defer close(ch)

		for stream.Next() {
			chunk := stream.Current()
			for _, choice := range chunk.Choices {
				ch <- Chunk{
					Content: choice.Delta.Content,
					Done:    choice.FinishReason == "stop" || choice.FinishReason == "length",
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- Chunk{Error: fmt.Errorf("openai stream: %w", err)}
		}
	}()

	return ch, nil
}

// resolveModel returns the model to use for this request.
// Request-level model overrides the provider default.
func (p *OpenAICompatibleProvider) resolveModel(opts *ChatOptions) string {
	if opts != nil && opts.Model != "" {
		return opts.Model
	}
	return p.model
}

// convertMessages converts internal Message format to OpenAI SDK format.
func (p *OpenAICompatibleProvider) convertMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		result = append(result, p.convertMessage(msg))
	}
	return result
}

// convertMessage converts a single internal Message to OpenAI SDK format.
func (p *OpenAICompatibleProvider) convertMessage(msg Message) openai.ChatCompletionMessageParamUnion {
	switch msg.Role {
	case "system":
		return openai.SystemMessage(msg.Content)
	case "user":
		return openai.UserMessage(msg.Content)
	case "assistant":
		return openai.AssistantMessage(msg.Content)
	default:
		return openai.UserMessage(msg.Content)
	}
}

// parseResponse converts an OpenAI SDK ChatCompletion to our ChatResponse.
//
// Some models (notably gpt-oss via Ollama) return empty message.content and put
// the actionable output in message.tool_calls — even when the request did not
// declare a tools schema. Our agent loop expects a text decision JSON in
// Content, so we synthesize that from the first tool call when needed.
//
// Reasoning-only fields (reasoning / thinking / reasoning_content) are also
// recovered when content is empty so parse dumps are not blank and retries
// can show the model what it produced.
func (p *OpenAICompatibleProvider) parseResponse(resp *openai.ChatCompletion) *ChatResponse {
	result := &ChatResponse{
		Usage: TokenUsage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
		},
	}

	if len(resp.Choices) == 0 {
		return result
	}

	msg := resp.Choices[0].Message
	content := strings.TrimSpace(msg.Content)

	if content == "" && len(msg.ToolCalls) > 0 {
		thought := reasoningFromMessage(msg)
		content = decisionJSONFromToolCall(msg.ToolCalls[0], thought)
	}

	// Still empty: surface refusal or reasoning so callers/dumps are not blank.
	if content == "" {
		switch {
		case strings.TrimSpace(msg.Refusal) != "":
			content = strings.TrimSpace(msg.Refusal)
		default:
			if thought := reasoningFromMessage(msg); thought != "" {
				content = thought
			} else if raw := strings.TrimSpace(msg.RawJSON()); raw != "" && raw != "null" {
				// Last resort: include the raw message object for diagnostics.
				content = raw
			}
		}
	}

	result.Content = content
	return result
}

// decisionJSONFromToolCall maps an OpenAI-style function tool call into the
// ReAct decision JSON the agent parser expects:
//
//	{"thought":"...","action":"<name>","args":{...}}
func decisionJSONFromToolCall(tc openai.ChatCompletionMessageToolCall, thought string) string {
	name := strings.TrimSpace(tc.Function.Name)
	argsRaw := strings.TrimSpace(tc.Function.Arguments)

	args := map[string]any{}
	if argsRaw != "" {
		if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
			// Keep the raw arguments string so the agent / dump can inspect it.
			args = map[string]any{"_raw": argsRaw}
		}
	}
	if thought == "" {
		thought = "native tool_calls (content empty)"
	}

	dec := map[string]any{
		"thought": thought,
		"action":  name,
		"args":    args,
	}
	b, err := json.Marshal(dec)
	if err != nil {
		// Extremely defensive: never return empty when we had a tool call.
		return fmt.Sprintf(`{"thought":%q,"action":%q,"args":{}}`, thought, name)
	}
	return string(b)
}

// reasoningFromMessage extracts a reasoning/thinking string from the raw
// assistant message JSON. OpenAI-go does not model Ollama's "reasoning" or
// native "thinking" fields, but they are preserved in RawJSON().
func reasoningFromMessage(msg openai.ChatCompletionMessage) string {
	return reasoningFromRaw(msg.RawJSON())
}

func reasoningFromRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	for _, key := range []string{"reasoning", "thinking", "reasoning_content"} {
		if v, ok := m[key].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}
