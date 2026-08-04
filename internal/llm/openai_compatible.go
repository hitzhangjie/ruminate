package llm

import (
	"context"
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
func (p *OpenAICompatibleProvider) parseResponse(resp *openai.ChatCompletion) *ChatResponse {
	result := &ChatResponse{
		Usage: TokenUsage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
		},
	}

	if len(resp.Choices) > 0 {
		result.Content = resp.Choices[0].Message.Content
	}

	return result
}
