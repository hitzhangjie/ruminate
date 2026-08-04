package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewProvider(t *testing.T) {
	t.Run("NewProvider_Ollama", func(t *testing.T) {
		p, err := NewProvider("ollama", "http://localhost:11434", "gemma3:4b", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("NewProvider_Ollama_DefaultBaseURL", func(t *testing.T) {
		p, err := NewProvider("ollama", "", "gemma3:4b", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("NewProvider_Unsupported", func(t *testing.T) {
		_, err := NewProvider("unknown", "", "", "")
		if err == nil {
			t.Fatal("expected error for unsupported provider")
		}
	})
}

// =============================================================================
// Mock-based unit tests — no real Ollama required
// =============================================================================

// openAICompatibleChatRequest mirrors the OpenAI chat completions request format.
type openAICompatibleChatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

func TestProviderChat_MockProvider(t *testing.T) {
	t.Run("Chat", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/chat/completions" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}

			var req openAICompatibleChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if req.Model != "gemma3:4b" {
				t.Errorf("expected model gemma3:4b, got %s", req.Model)
			}
			if req.Stream {
				t.Error("expected stream=false")
			}
			if len(req.Messages) != 2 {
				t.Errorf("expected 2 messages, got %d", len(req.Messages))
			}

			// OpenAI-compatible response format
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-123",
				"object":  "chat.completion",
				"model":   "gemma3:4b",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]string{
							"role":    "assistant",
							"content": "Hello, world!",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]int{
					"prompt_tokens":     10,
					"completion_tokens": 5,
					"total_tokens":      15,
				},
			})
		}))
		defer server.Close()

		p, err := NewOllamaProvider(server.URL, "gemma3:4b")
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		resp, err := p.Chat(context.Background(), []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi!"},
		}, &ChatOptions{Temperature: 0.3})

		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
		if resp.Content != "Hello, world!" {
			t.Errorf("expected 'Hello, world!', got %q", resp.Content)
		}
		if resp.Usage.PromptTokens != 10 {
			t.Errorf("expected 10 prompt tokens, got %d", resp.Usage.PromptTokens)
		}
		if resp.Usage.CompletionTokens != 5 {
			t.Errorf("expected 5 completion tokens, got %d", resp.Usage.CompletionTokens)
		}
	})

	t.Run("ChatStream", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("expected ResponseWriter to be a Flusher")
			}

			var req openAICompatibleChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if !req.Stream {
				t.Error("expected stream=true")
			}

			// SSE streaming format (OpenAI-compatible)
			chunks := []string{
				`{"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gemma3:4b","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
				`{"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gemma3:4b","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
				`{"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gemma3:4b","choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}]}`,
			}
			for _, chunk := range chunks {
				fmt.Fprintf(w, "data: %s\n\n", chunk)
				flusher.Flush()
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}))
		defer server.Close()

		p, err := NewOllamaProvider(server.URL, "gemma3:4b")
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		ch, err := p.ChatStream(context.Background(), []Message{
			{Role: "user", Content: "Say hello"},
		}, nil)

		if err != nil {
			t.Fatalf("ChatStream failed: %v", err)
		}

		var result string
		for chunk := range ch {
			if chunk.Error != nil {
				t.Fatalf("stream error: %v", chunk.Error)
			}
			result += chunk.Content
		}

		if result != "Hello world" {
			t.Errorf("expected 'Hello world', got %q", result)
		}
	})

	t.Run("Chat_ErrorStatus", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"internal error","type":"server_error"}}`))
		}))
		defer server.Close()

		p, err := NewOllamaProvider(server.URL, "gemma3:4b")
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		_, err = p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
		if err == nil {
			t.Fatal("expected error for 500 status")
		}
	})

	t.Run("Chat_ModelOverride", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req openAICompatibleChatRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Model != "custom-model" {
				t.Errorf("expected model 'custom-model', got %s", req.Model)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "chatcmpl-123",
				"object": "chat.completion",
				"model":  "custom-model",
				"choices": []map[string]any{
					{
						"index":         0,
						"message":       map[string]string{"role": "assistant", "content": "ok"},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]int{
					"prompt_tokens":     1,
					"completion_tokens": 1,
					"total_tokens":      2,
				},
			})
		}))
		defer server.Close()

		p, err := NewOllamaProvider(server.URL, "gemma3:4b")
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		_, err = p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, &ChatOptions{Model: "custom-model"})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
	})

	// gpt-oss / tool-capable models often return empty content + tool_calls.
	// Provider must synthesize ReAct decision JSON so the agent can parse it.
	t.Run("Chat_EmptyContent_ToolCalls", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "chatcmpl-tc",
				"object": "chat.completion",
				"model":  "gpt-oss:20b",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"reasoning": "Need to search the wiki for fmt.Errorf optimization.",
							"tool_calls": []map[string]any{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]string{
										"name":      "wiki_search",
										"arguments": `{"query":"fmt.Errorf Go 1.26","top_n":8}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
				"usage": map[string]int{
					"prompt_tokens":     100,
					"completion_tokens": 50,
					"total_tokens":      150,
				},
			})
		}))
		defer server.Close()

		p, err := NewOllamaProvider(server.URL, "gpt-oss:20b")
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Go 1.26 fmt.Errorf?"}}, nil)
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
		if resp.Content == "" {
			t.Fatal("expected synthesized content from tool_calls, got empty")
		}

		var dec struct {
			Thought string         `json:"thought"`
			Action  string         `json:"action"`
			Args    map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(resp.Content), &dec); err != nil {
			t.Fatalf("synthesized content is not JSON: %v\ncontent=%s", err, resp.Content)
		}
		if dec.Action != "wiki_search" {
			t.Errorf("action = %q, want wiki_search", dec.Action)
		}
		if dec.Args["query"] != "fmt.Errorf Go 1.26" {
			t.Errorf("args.query = %v, want fmt.Errorf Go 1.26", dec.Args["query"])
		}
		if !strings.Contains(dec.Thought, "fmt.Errorf") && !strings.Contains(dec.Thought, "tool_calls") {
			t.Errorf("thought should include reasoning or fallback, got %q", dec.Thought)
		}
		if resp.Usage.PromptTokens != 100 {
			t.Errorf("prompt tokens = %d, want 100", resp.Usage.PromptTokens)
		}
	})

	t.Run("Chat_EmptyContent_ReasoningOnly", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "chatcmpl-reason",
				"object": "chat.completion",
				"model":  "gpt-oss:20b",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":      "assistant",
							"content":   "",
							"reasoning": "I am thinking about the answer but produced no content.",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]int{
					"prompt_tokens":     10,
					"completion_tokens": 20,
					"total_tokens":      30,
				},
			})
		}))
		defer server.Close()

		p, err := NewOllamaProvider(server.URL, "gpt-oss:20b")
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
		if resp.Content == "" {
			t.Fatal("expected reasoning fallback content, got empty")
		}
		if !strings.Contains(resp.Content, "thinking about the answer") {
			t.Errorf("content = %q, want reasoning text", resp.Content)
		}
	})
}

// =============================================================================
// Integration tests — require a real Ollama instance
// =============================================================================

func TestProviderChat_Ollama(t *testing.T) {
	skipIfOllamaUnavailable(t)

	baseURL := ollamaBaseURL()
	model := ollamaTestModel()

	t.Run("Chat", func(t *testing.T) {
		p, err := NewOllamaProvider(baseURL, model)
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		resp, err := p.Chat(context.Background(), []Message{
			{Role: "system", Content: "You are a helpful assistant. Always reply with exactly one sentence."},
			{Role: "user", Content: "What is the capital of France?"},
		}, &ChatOptions{Temperature: 0})

		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
		if resp.Content == "" {
			t.Fatal("expected non-empty response content")
		}
		if resp.Usage.PromptTokens <= 0 {
			t.Errorf("expected positive prompt tokens, got %d", resp.Usage.PromptTokens)
		}
		if resp.Usage.CompletionTokens <= 0 {
			t.Errorf("expected positive completion tokens, got %d", resp.Usage.CompletionTokens)
		}

		t.Logf("Response: %s", resp.Content)
		t.Logf("Usage: prompt=%d completion=%d", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	})

	t.Run("ChatStream", func(t *testing.T) {
		p, err := NewOllamaProvider(baseURL, model)
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}
		ch, err := p.ChatStream(context.Background(), []Message{
			{Role: "system", Content: "You are a helpful assistant. Keep responses short."},
			{Role: "user", Content: "Say hello in exactly 3 words."},
		}, &ChatOptions{Temperature: 0})

		if err != nil {
			t.Fatalf("ChatStream failed: %v", err)
		}

		var result string
		chunkCount := 0
		for chunk := range ch {
			if chunk.Error != nil {
				t.Fatalf("stream error: %v", chunk.Error)
			}
			result += chunk.Content
			chunkCount++
		}

		if result == "" {
			t.Fatal("expected non-empty streamed response")
		}
		if chunkCount == 0 {
			t.Fatal("expected at least one chunk")
		}

		t.Logf("Streamed response (%d chunks): %s", chunkCount, result)
	})

	t.Run("Chat_WithContext", func(t *testing.T) {
		p, err := NewOllamaProvider(baseURL, model)
		if err != nil {
			t.Fatalf("NewOllamaProvider failed: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := p.Chat(ctx, []Message{
			{Role: "user", Content: "Reply with just the word: OK"},
		}, &ChatOptions{Temperature: 0, MaxTokens: 5})

		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
		if resp.Content == "" {
			t.Fatal("expected non-empty response")
		}

		t.Logf("Response: %s", resp.Content)
	})
}

// ollamaBaseURL returns the Ollama base URL from OLLAMA_BASE_URL env var,
// falling back to http://localhost:11434.
func ollamaBaseURL() string {
	if u := os.Getenv("OLLAMA_BASE_URL"); u != "" {
		return u
	}
	return "http://localhost:11434"
}

// ollamaTestModel returns the test model from OLLAMA_TEST_MODEL env var,
// falling back to gemma3:4b.
func ollamaTestModel() string {
	if m := os.Getenv("OLLAMA_TEST_MODEL"); m != "" {
		return m
	}
	return "gemma3:4b"
}

// skipIfOllamaUnavailable skips the test if Ollama is not reachable.
func skipIfOllamaUnavailable(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaBaseURL() + "/api/tags")
	if err != nil {
		t.Skipf("ollama not available at %s: %v", ollamaBaseURL(), err)
	}
	defer resp.Body.Close()
}
