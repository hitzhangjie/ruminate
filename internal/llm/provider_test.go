package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestProviderChat_MockProvider(t *testing.T) {
	t.Run("Chat", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/chat" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}

			var req ollamaChatRequest
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

			resp := ollamaChatResponse{
				Model: "gemma3:4b",
				Message: ollamaMessage{
					Role:    "assistant",
					Content: "Hello, world!",
				},
				Done:            true,
				EvalCount:       5,
				PromptEvalCount: 10,
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewOllamaProvider(server.URL, "gemma3:4b")
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

			var req ollamaChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if !req.Stream {
				t.Error("expected stream=true")
			}

			chunks := []string{
				`{"model":"gemma3:4b","message":{"role":"assistant","content":"Hello"},"done":false}`,
				`{"model":"gemma3:4b","message":{"role":"assistant","content":" world"},"done":false}`,
				`{"model":"gemma3:4b","message":{"role":"assistant","content":""},"done":true}`,
			}
			for _, chunk := range chunks {
				w.Write([]byte(chunk + "\n"))
				flusher.Flush()
			}
		}))
		defer server.Close()

		p := NewOllamaProvider(server.URL, "gemma3:4b")
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
			w.Write([]byte("internal error"))
		}))
		defer server.Close()

		p := NewOllamaProvider(server.URL, "gemma3:4b")
		_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
		if err == nil {
			t.Fatal("expected error for 500 status")
		}
	})

	t.Run("Chat_ModelOverride", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req ollamaChatRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Model != "custom-model" {
				t.Errorf("expected model 'custom-model', got %s", req.Model)
			}
			resp := ollamaChatResponse{
				Model:   "custom-model",
				Message: ollamaMessage{Role: "assistant", Content: "ok"},
				Done:    true,
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewOllamaProvider(server.URL, "gemma3:4b")
		_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, &ChatOptions{Model: "custom-model"})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
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
		p := NewOllamaProvider(baseURL, model)
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
		p := NewOllamaProvider(baseURL, model)
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
		p := NewOllamaProvider(baseURL, model)

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
