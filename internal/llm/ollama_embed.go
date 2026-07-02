package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OllamaEmbedder implements EmbeddingProvider for Ollama.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaEmbedder creates a new OllamaEmbedder.
func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

// ollamaEmbedRequest is the JSON body for POST /api/embed.
type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// ollamaEmbedResponse is the JSON body from POST /api/embed.
type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
}

// Embed converts a batch of texts to vectors using Ollama's embed API.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	req := ollamaEmbedRequest{
		Model: e.model,
		Input: texts,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decoding embed response: %w", err)
	}

	// Convert [][]float64 → [][]float32
	embeddings := make([][]float32, len(embedResp.Embeddings))
	for i, vec := range embedResp.Embeddings {
		embeddings[i] = make([]float32, len(vec))
		for j, v := range vec {
			embeddings[i][j] = float32(v)
		}
	}

	return embeddings, nil
}

// EmbedQuery converts a single query text to a vector.
// For Ollama, this is the same as Embed with a single-element batch.
func (e *OllamaEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("empty embeddings response")
	}
	return embeddings[0], nil
}
