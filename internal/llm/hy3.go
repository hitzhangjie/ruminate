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

// Hy3Provider implements LLMProvider for Tencent HY3 API (OpenAI-compatible).
//
// HY3 API docs: the Chat Completions API endpoint is at
//
//	POST /openapi/v2/chat/completions
//
// with base URL http://api.taiji.woa.com.
// It uses Bearer token auth and is largely OpenAI-compatible.
type Hy3Provider struct {
	baseURL string
	model   string
	client  openai.Client
}

// NewHy3Provider creates a new Hy3Provider.
//
// baseURL should be the full base URL including the API version path,
// e.g. "http://api.taiji.woa.com/openapi/v2".
// model is the model name, typically "hy3".
// apiKey is the Bearer token for authentication.
func NewHy3Provider(baseURL, model, apiKey string) (*Hy3Provider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("hy3: api key is required")
	}

	baseURL = strings.TrimSuffix(baseURL, "/")

	var opts []option.RequestOption
	opts = append(opts, option.WithAPIKey(apiKey))
	opts = append(opts, option.WithBaseURL(baseURL))

	client := openai.NewClient(opts...)

	return &Hy3Provider{
		baseURL: baseURL,
		model:   model,
		client:  client,
	}, nil
}

// Chat sends a non-streaming chat request to HY3 API.
func (p *Hy3Provider) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error) {
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
		return nil, fmt.Errorf("hy3 chat: %w", err)
	}

	return p.parseResponse(resp), nil
}

// ChatStream sends a streaming chat request to HY3 API.
func (p *Hy3Provider) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan Chunk, error) {
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

	// Enable usage chunk in streaming (HY3 returns usage in a final chunk when
	// stream_options.include_usage is true).
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
			ch <- Chunk{Error: fmt.Errorf("hy3 stream: %w", err)}
		}
	}()

	return ch, nil
}

// resolveModel returns the model to use for this request.
// Request-level model overrides the provider default.
func (p *Hy3Provider) resolveModel(opts *ChatOptions) string {
	if opts != nil && opts.Model != "" {
		return opts.Model
	}
	return p.model
}

// convertMessages converts internal Message format to OpenAI SDK format.
func (p *Hy3Provider) convertMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		result = append(result, p.convertMessage(msg))
	}
	return result
}

// convertMessage converts a single internal Message to OpenAI SDK format.
func (p *Hy3Provider) convertMessage(msg Message) openai.ChatCompletionMessageParamUnion {
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
func (p *Hy3Provider) parseResponse(resp *openai.ChatCompletion) *ChatResponse {
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
