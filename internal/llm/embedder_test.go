package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// ollamaEmbedModel returns the embedding model from OLLAMA_EMBED_MODEL env var,
// falling back to nomic-embed-text.
func ollamaEmbedModel() string {
	if m := os.Getenv("OLLAMA_EMBED_MODEL"); m != "" {
		return m
	}
	return "nomic-embed-text"
}

func TestNewEmbeddingProvider(t *testing.T) {
	t.Run("NewProvider_Ollama", func(t *testing.T) {
		p, err := NewEmbeddingProvider("ollama", "http://localhost:11434", "nomic-embed-text")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("NewProvider_Unsupported", func(t *testing.T) {
		_, err := NewEmbeddingProvider("unknown", "", "")
		if err == nil {
			t.Fatal("expected error for unsupported provider")
		}
	})
}

// =============================================================================
// Mock-based unit tests — no real Ollama required
// =============================================================================

func TestEmbedder_MockProvider(t *testing.T) {
	t.Run("Embed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/embed" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}

			var req ollamaEmbedRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if req.Model != "nomic-embed-text" {
				t.Errorf("expected model nomic-embed-text, got %s", req.Model)
			}
			if len(req.Input) != 2 {
				t.Errorf("expected 2 inputs, got %d", len(req.Input))
			}

			resp := ollamaEmbedResponse{
				Model: "nomic-embed-text",
				Embeddings: [][]float64{
					{0.1, 0.2, 0.3},
					{0.4, 0.5, 0.6},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		e := NewOllamaEmbedder(server.URL, "nomic-embed-text")
		vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}
		if len(vecs) != 2 {
			t.Fatalf("expected 2 vectors, got %d", len(vecs))
		}
		if len(vecs[0]) != 3 {
			t.Errorf("expected 3 dimensions, got %d", len(vecs[0]))
		}
	})

	t.Run("EmbedQuery", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := ollamaEmbedResponse{
				Model:      "nomic-embed-text",
				Embeddings: [][]float64{{0.1, 0.2, 0.3}},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		e := NewOllamaEmbedder(server.URL, "nomic-embed-text")
		vec, err := e.EmbedQuery(context.Background(), "hello")
		if err != nil {
			t.Fatalf("EmbedQuery failed: %v", err)
		}
		if len(vec) != 3 {
			t.Errorf("expected 3 dimensions, got %d", len(vec))
		}
	})

	t.Run("Embed_ErrorStatus", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("bad request"))
		}))
		defer server.Close()

		e := NewOllamaEmbedder(server.URL, "nomic-embed-text")
		_, err := e.Embed(context.Background(), []string{"test"})
		if err == nil {
			t.Fatal("expected error for 400 status")
		}
	})
}

// =============================================================================
// Integration tests — require a real Ollama instance
// =============================================================================

func TestEmbedder_Ollama(t *testing.T) {
	skipIfOllamaUnavailable(t)

	baseURL := ollamaBaseURL()
	model := ollamaEmbedModel()

	t.Run("Embed", func(t *testing.T) {
		e := NewOllamaEmbedder(baseURL, model)
		vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}
		if len(vecs) != 2 {
			t.Fatalf("expected 2 vectors, got %d", len(vecs))
		}
		if len(vecs[0]) == 0 {
			t.Fatal("expected non-empty embedding vector")
		}
		// All vectors should have the same dimensionality.
		dim := len(vecs[0])
		for i, v := range vecs {
			if len(v) != dim {
				t.Errorf("vector %d: expected %d dims, got %d", i, dim, len(v))
			}
		}

		t.Logf("Model: %s, vectors: %d, dims: %d", model, len(vecs), dim)
	})

	t.Run("EmbedQuery", func(t *testing.T) {
		e := NewOllamaEmbedder(baseURL, model)
		vec, err := e.EmbedQuery(context.Background(), "What is the meaning of life?")
		if err != nil {
			t.Fatalf("EmbedQuery failed: %v", err)
		}
		if len(vec) == 0 {
			t.Fatal("expected non-empty embedding vector")
		}

		t.Logf("Model: %s, dims: %d", model, len(vec))
	})

	t.Run("Embed_EmptyInput", func(t *testing.T) {
		e := NewOllamaEmbedder(baseURL, model)
		vecs, err := e.Embed(context.Background(), []string{})
		if err != nil {
			t.Fatalf("Embed with empty input failed: %v", err)
		}
		// Ollama may return empty or error for empty input — either is acceptable
		// as long as it doesn't panic.
		t.Logf("Empty input returned %d vectors", len(vecs))
	})
}
