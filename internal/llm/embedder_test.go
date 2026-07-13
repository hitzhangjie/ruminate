package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
		p, err := NewEmbeddingProvider("ollama", "http://localhost:11434", "nomic-embed-text", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("NewProvider_Ollama_DefaultBaseURL", func(t *testing.T) {
		p, err := NewEmbeddingProvider("ollama", "", "nomic-embed-text", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("NewProvider_Unsupported", func(t *testing.T) {
		_, err := NewEmbeddingProvider("unknown", "", "", "")
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

// =============================================================================
// HunyuanEmbedder mock-based unit tests
// =============================================================================

// mockEmbeddingResponse is the minimal OpenAI-compatible embedding response.
type mockEmbeddingResponse struct {
	Object string              `json:"object"`
	Data   []mockEmbeddingData `json:"data"`
	Model  string              `json:"model"`
	Usage  mockEmbeddingUsage  `json:"usage"`
}

type mockEmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type mockEmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func newMockEmbeddingServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The OpenAI SDK POSTs to /embeddings relative to the base URL.
		if !strings.HasSuffix(r.URL.Path, "/embeddings") && r.URL.Path != "/embeddings" {
			t.Logf("unexpected path: %s", r.URL.Path)
		}

		// Verify auth header.
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-api-key" {
			t.Errorf("expected Authorization: Bearer test-api-key, got %q", auth)
		}

		var req struct {
			Model string `json:"model"`
			Input any    `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "hunyuan-embedding-20250716" {
			t.Errorf("expected model hunyuan-embedding-20250716, got %q", req.Model)
		}

		// Determine number of inputs from the request.
		numInputs := 1
		switch v := req.Input.(type) {
		case string:
			numInputs = 1
		case []any:
			numInputs = len(v)
		}

		// Build response with the correct number of embeddings.
		data := make([]mockEmbeddingData, numInputs)
		for i := 0; i < numInputs; i++ {
			data[i] = mockEmbeddingData{
				Object:    "embedding",
				Embedding: []float64{float64(i) * 0.1, 0.2, 0.3},
				Index:     i,
			}
		}

		resp := mockEmbeddingResponse{
			Object: "list",
			Data:   data,
			Model:  "hunyuan-embedding-20250716",
			Usage: mockEmbeddingUsage{
				PromptTokens: 5 * numInputs,
				TotalTokens:  5 * numInputs,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestHunyuanEmbedder_New(t *testing.T) {
	t.Run("requires_api_key", func(t *testing.T) {
		_, err := NewHunyuanEmbedder("http://hunyuanapi.woa.com/openapi/v1", "hunyuan-embedding-20250716", "")
		if err == nil {
			t.Fatal("expected error for empty api key")
		}
		if !strings.Contains(err.Error(), "api key") {
			t.Errorf("expected error about api key, got: %v", err)
		}
	})

	t.Run("creates_provider", func(t *testing.T) {
		p, err := NewHunyuanEmbedder("http://hunyuanapi.woa.com/openapi/v1", "hunyuan-embedding-20250716", "test-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("factory_registered", func(t *testing.T) {
		p, err := NewEmbeddingProvider("hunyuan", "http://hunyuanapi.woa.com/openapi/v1", "hunyuan-embedding-20250716", "test-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider from factory")
		}
	})
}

func TestHunyuanEmbedder_Embed(t *testing.T) {
	t.Run("single_short_text", func(t *testing.T) {
		server := newMockEmbeddingServer(t)
		defer server.Close()

		e, err := NewHunyuanEmbedder(server.URL, "hunyuan-embedding-20250716", "test-api-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		vecs, err := e.Embed(context.Background(), []string{"hello world"})
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}
		if len(vecs) != 1 {
			t.Fatalf("expected 1 vector, got %d", len(vecs))
		}
		if len(vecs[0]) != 3 {
			t.Errorf("expected 3 dimensions, got %d", len(vecs[0]))
		}
	})

	t.Run("multiple_short_texts", func(t *testing.T) {
		server := newMockEmbeddingServer(t)
		defer server.Close()

		e, err := NewHunyuanEmbedder(server.URL, "hunyuan-embedding-20250716", "test-api-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		vecs, err := e.Embed(context.Background(), []string{"hello", "world", "foo"})
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}
		if len(vecs) != 3 {
			t.Fatalf("expected 3 vectors, got %d", len(vecs))
		}
		for i, v := range vecs {
			if len(v) != 3 {
				t.Errorf("vector %d: expected 3 dimensions, got %d", i, len(v))
			}
		}
	})

	t.Run("long_text_chunking", func(t *testing.T) {
		server := newMockEmbeddingServer(t)
		defer server.Close()

		e, err := NewHunyuanEmbedder(server.URL, "hunyuan-embedding-20250716", "test-api-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Generate text that exceeds maxCharsPerChunk (1500 chars).
		// Each paragraph is ~200 chars, so we need ~10 paragraphs.
		paragraph := strings.Repeat("这是一段测试文本，用于验证分块功能是否正常工作。", 5)
		longText := strings.Repeat(paragraph+"\n\n", 10)

		vecs, err := e.Embed(context.Background(), []string{longText})
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}
		if len(vecs) != 1 {
			t.Fatalf("expected 1 vector (mean-pooled from chunks), got %d", len(vecs))
		}
		if len(vecs[0]) != 3 {
			t.Errorf("expected 3 dimensions, got %d", len(vecs[0]))
		}

		t.Logf("Long text (%d chars) → 1 vector of %d dims (mean-pooled)", len(longText), len(vecs[0]))
	})

	t.Run("empty_input", func(t *testing.T) {
		server := newMockEmbeddingServer(t)
		defer server.Close()

		e, err := NewHunyuanEmbedder(server.URL, "hunyuan-embedding-20250716", "test-api-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		vecs, err := e.Embed(context.Background(), []string{})
		if err != nil {
			t.Fatalf("Embed with empty input failed: %v", err)
		}
		if vecs != nil {
			t.Logf("Empty input returned %d vectors", len(vecs))
		}
	})
}

func TestHunyuanEmbedder_EmbedQuery(t *testing.T) {
	t.Run("short_query", func(t *testing.T) {
		server := newMockEmbeddingServer(t)
		defer server.Close()

		e, err := NewHunyuanEmbedder(server.URL, "hunyuan-embedding-20250716", "test-api-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		vec, err := e.EmbedQuery(context.Background(), "什么是Rust？")
		if err != nil {
			t.Fatalf("EmbedQuery failed: %v", err)
		}
		if len(vec) != 3 {
			t.Errorf("expected 3 dimensions, got %d", len(vec))
		}
	})
}

func TestHunyuanEmbedder_ErrorHandling(t *testing.T) {
	t.Run("non_200_status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": {"message": "invalid api key"}}`))
		}))
		defer server.Close()

		e, err := NewHunyuanEmbedder(server.URL, "hunyuan-embedding-20250716", "test-api-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = e.Embed(context.Background(), []string{"test"})
		if err == nil {
			t.Fatal("expected error for 401 status")
		}
	})
}

// =============================================================================
// Chunking unit tests
// =============================================================================

func TestChunkText(t *testing.T) {
	t.Run("empty_string", func(t *testing.T) {
		chunks := chunkText("", 100)
		if len(chunks) != 1 || chunks[0] != "" {
			t.Errorf("expected [\"\"], got %v", chunks)
		}
	})

	t.Run("short_text", func(t *testing.T) {
		text := "hello world"
		chunks := chunkText(text, 100)
		if len(chunks) != 1 || chunks[0] != text {
			t.Errorf("expected 1 chunk, got %d: %v", len(chunks), chunks)
		}
	})

	t.Run("paragraph_split", func(t *testing.T) {
		para := "This is a test paragraph."
		text := strings.Repeat(para+"\n\n", 5)
		chunks := chunkText(text, 40)

		if len(chunks) < 2 {
			t.Errorf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
		}
	})

	t.Run("long_paragraph_split_at_sentences", func(t *testing.T) {
		sentence := "这是第一句话。这是第二句话！这是第三句话？"
		text := strings.Repeat(sentence, 20)
		chunks := chunkText(text, 200)

		if len(chunks) < 2 {
			t.Errorf("expected at least 2 chunks, got %d", len(chunks))
		}
	})

	t.Run("no_sentence_boundaries", func(t *testing.T) {
		text := strings.Repeat("abcdefghij", 200) // 2000 chars, no punctuation
		chunks := chunkText(text, 500)

		if len(chunks) < 2 {
			t.Errorf("expected at least 2 chunks, got %d", len(chunks))
		}
		for i, c := range chunks {
			if len([]rune(c)) > 500 {
				t.Errorf("chunk %d: %d runes exceeds limit %d", i, len([]rune(c)), 500)
			}
		}
	})
}

func TestMeanPool(t *testing.T) {
	t.Run("single_vector", func(t *testing.T) {
		vecs := [][]float32{{1.0, 2.0, 3.0}}
		result := meanPool(vecs)
		if len(result) != 3 || result[0] != 1.0 || result[1] != 2.0 || result[2] != 3.0 {
			t.Errorf("expected [1,2,3], got %v", result)
		}
	})

	t.Run("two_vectors", func(t *testing.T) {
		vecs := [][]float32{
			{1.0, 2.0, 3.0},
			{3.0, 4.0, 5.0},
		}
		result := meanPool(vecs)
		expected := []float32{2.0, 3.0, 4.0}
		for i, v := range expected {
			if result[i] != v {
				t.Errorf("index %d: expected %v, got %v", i, expected, result)
				break
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		result := meanPool(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})
}

func TestSplitSentences(t *testing.T) {
	t.Run("chinese", func(t *testing.T) {
		text := "第一句话。第二句话！第三句话？"
		sents := splitSentences(text)
		if len(sents) != 3 {
			t.Errorf("expected 3 sentences, got %d: %v", len(sents), sents)
		}
	})

	t.Run("english", func(t *testing.T) {
		text := "First sentence. Second sentence! Third sentence?"
		sents := splitSentences(text)
		if len(sents) != 3 {
			t.Errorf("expected 3 sentences, got %d: %v", len(sents), sents)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		text := "这是中文。This is English. 第二句中文！More English!"
		sents := splitSentences(text)
		if len(sents) != 4 {
			t.Errorf("expected 4 sentences, got %d: %v", len(sents), sents)
		}
	})

	t.Run("no_boundaries", func(t *testing.T) {
		text := "This is a continuous text without any sentence boundaries"
		sents := splitSentences(text)
		if len(sents) != 1 {
			t.Errorf("expected 1 sentence, got %d: %v", len(sents), sents)
		}
	})
}
