package llm

import "context"

// EmbeddingProvider is the interface for text embedding backends.
type EmbeddingProvider interface {
	// Embed converts a batch of texts to vectors.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// EmbedQuery converts a single query text to a vector.
	// Some models use different encoding for queries vs documents.
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// NewEmbeddingProvider creates an embedding provider based on the config.
// Supported providers: "ollama".
func NewEmbeddingProvider(provider, baseURL, model, apikey string) (EmbeddingProvider, error) {
	switch provider {
	case "ollama":
		return NewOllamaEmbedder(baseURL, model), nil
	default:
		return nil, &ErrUnsupportedProvider{Provider: provider}
	}
}
