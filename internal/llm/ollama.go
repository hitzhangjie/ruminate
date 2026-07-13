package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultOllamaBaseURL = "http://localhost:11434"

// OllamaProvider implements LLMProvider for Ollama.
type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaProvider creates a new OllamaProvider.
// baseURL is the Ollama API endpoint (e.g. "http://localhost:11434").
// If empty, defaults to http://localhost:11434.
// model is the default model name (e.g. "llama3.2").
func NewOllamaProvider(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	return &OllamaProvider{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

// ollamaChatRequest is the JSON body for POST /api/chat.
type ollamaChatRequest struct {
	Model    string            `json:"model"`
	Messages []ollamaMessage   `json:"messages"`
	Stream   bool              `json:"stream"`
	Options  *ollamaOptions    `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

// ollamaChatResponse is the JSON body from a non-streaming response.
type ollamaChatResponse struct {
	Model     string        `json:"model"`
	Message   ollamaMessage `json:"message"`
	Done      bool          `json:"done"`
	EvalCount int           `json:"eval_count"`
	PromptEvalCount int     `json:"prompt_eval_count"`
}

// ollamaStreamChunk is a single line from a streaming response.
type ollamaStreamChunk struct {
	Model   string        `json:"model"`
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// Chat sends a non-streaming chat request to Ollama.
func (p *OllamaProvider) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error) {
	model := p.model
	if opts != nil && opts.Model != "" {
		model = opts.Model
	}

	req := ollamaChatRequest{
		Model:    model,
		Messages: toOllamaMessages(messages),
		Stream:   false,
	}
	if opts != nil {
		req.Options = &ollamaOptions{
			Temperature: opts.Temperature,
			NumPredict:  opts.MaxTokens,
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &ChatResponse{
		Content: chatResp.Message.Content,
		Usage: TokenUsage{
			PromptTokens:     chatResp.PromptEvalCount,
			CompletionTokens: chatResp.EvalCount,
		},
	}, nil
}

// ChatStream sends a streaming chat request to Ollama.
// The returned channel emits chunks until Done or Error, then is closed.
func (p *OllamaProvider) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan Chunk, error) {
	model := p.model
	if opts != nil && opts.Model != "" {
		model = opts.Model
	}

	req := ollamaChatRequest{
		Model:    model,
		Messages: toOllamaMessages(messages),
		Stream:   true,
	}
	if opts != nil {
		req.Options = &ollamaOptions{
			Temperature: opts.Temperature,
			NumPredict:  opts.MaxTokens,
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	ch := make(chan Chunk, 10)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		// Ollama streaming responses can be large per line; bump buffer
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var chunk ollamaStreamChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				ch <- Chunk{Error: fmt.Errorf("decoding stream chunk: %w", err)}
				return
			}

			ch <- Chunk{
				Content: chunk.Message.Content,
				Done:    chunk.Done,
			}

			if chunk.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- Chunk{Error: fmt.Errorf("reading stream: %w", err)}
		}
	}()

	return ch, nil
}

func toOllamaMessages(messages []Message) []ollamaMessage {
	result := make([]ollamaMessage, len(messages))
	for i, m := range messages {
		result[i] = ollamaMessage{Role: m.Role, Content: m.Content}
	}
	return result
}
