package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OllamaEmbedder implements EmbeddingProvider for Ollama (OpenAI-compatible).
//
// Ollama exposes OpenAI-compatible /v1/embeddings endpoint (see
// https://docs.ollama.com/api/openai-compatibility), so we use the
// openai-go SDK — the same pattern as HunyuanEmbedder.
type OllamaEmbedder struct {
	client openai.Client
	model  string
}

// NewOllamaEmbedder creates a new OllamaEmbedder.
//
// baseURL is the Ollama API endpoint (e.g. "http://localhost:11434").
// If empty, defaults to http://localhost:11434.
// model is the embedding model name (e.g. "nomic-embed-text").
//
// Ollama ignores the Authorization header, so we pass a dummy apiKey.
func NewOllamaEmbedder(baseURL, model string) (*OllamaEmbedder, error) {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	var opts []option.RequestOption
	opts = append(opts, option.WithAPIKey("ollama")) // Ollama ignores auth
	opts = append(opts, option.WithBaseURL(baseURL+"/v1"))

	return &OllamaEmbedder{
		client: openai.NewClient(opts...),
		model:  model,
	}, nil
}

// Embed converts a batch of texts to vectors using Ollama's embedding API.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	params := openai.EmbeddingNewParams{
		Model: e.model,
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
	}

	resp, err := e.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("ollama embed: empty response data")
	}

	// Convert: sort by index (API may reorder), convert float64 → float32.
	vecs := make([][]float32, len(resp.Data))
	for _, emb := range resp.Data {
		if int(emb.Index) >= len(vecs) {
			return nil, fmt.Errorf("ollama embed: unexpected index %d (expected < %d)", emb.Index, len(vecs))
		}
		vecs[emb.Index] = float64ToFloat32(emb.Embedding)
	}

	return vecs, nil
}

// EmbedQuery converts a single query text to a vector.
func (e *OllamaEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("ollama embed: empty response")
	}
	return vecs[0], nil
}
