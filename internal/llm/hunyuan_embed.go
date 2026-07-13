package llm

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const (
	// hunyuanMaxInputTokens is the max input tokens per text for hunyuan-embedding-20250716.
	// The API auto-truncates inputs exceeding this limit.
	hunyuanMaxInputTokens = 1024

	// maxCharsPerChunk is a conservative estimate for 1024 tokens in worst-case
	// (Chinese-dominant) text. Chinese: ~1.5 chars/token → 1024*1.5 ≈ 1536 chars.
	// We use 1500 to stay safely within the limit.
	maxCharsPerChunk = 1500
)

// HunyuanEmbedder implements EmbeddingProvider for Tencent Hunyuan Embedding API.
//
// The Hunyuan Embedding API is OpenAI-compatible:
//
//	POST http://hunyuanapi.woa.com/openapi/v1/embeddings
//
// Note: the embedding API uses hunyuanapi.woa.com, which is different from
// the chat API (api.taiji.woa.com).
//
// Model: hunyuan-embedding-20250716. Max 1024 tokens per input (auto-truncated by
// the backend). Max 50 inputs per request. QPM (queries per minute): 10.
//
// Error codes:
//
//	400 — request format error (e.g. invalid JSON, missing required fields)
//	401 — authentication failed (invalid or expired API key)
//	429 — rate limit exceeded (QPM > 10)
//	500 — internal server error
//
// Long texts (>~1500 chars) are automatically chunked, each chunk embedded
// separately, and the resulting vectors mean-pooled into a single vector.
// This preserves semantic information that would otherwise be lost by the
// API's silent truncation.
type HunyuanEmbedder struct {
	client openai.Client
	model  string
}

const defaultHunyuanEmbedBaseURL = "http://hunyuanapi.woa.com/openapi/v1"

// NewHunyuanEmbedder creates a new HunyuanEmbedder.
//
// baseURL defaults to http://hunyuanapi.woa.com/openapi/v1 when empty.
// model is the model name, typically "hunyuan-embedding-20250716".
// apiKey is the Bearer token for authentication.
func NewHunyuanEmbedder(baseURL, model, apiKey string) (*HunyuanEmbedder, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("hunyuan embed: api key is required")
	}

	if baseURL == "" {
		baseURL = defaultHunyuanEmbedBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	var opts []option.RequestOption
	opts = append(opts, option.WithAPIKey(apiKey))
	opts = append(opts, option.WithBaseURL(baseURL))

	return &HunyuanEmbedder{
		client: openai.NewClient(opts...),
		model:  model,
	}, nil
}

// Embed converts a batch of texts to vectors.
//
// Each text that exceeds maxCharsPerChunk is split into chunks, each chunk is
// embedded via the API, and the chunk vectors are mean-pooled into a single
// vector for that text. Texts within the limit are embedded directly.
func (e *HunyuanEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Plan: for each text, determine its chunks.
	// Collect all chunks across all texts and send one batch request.
	type textPlan struct {
		origIdx    int // index in the original texts slice
		chunkStart int // first chunk index in the flat chunks slice
		chunkEnd   int // exclusive: chunks[chunkStart:chunkEnd] belong to this text
	}
	var plans []textPlan
	var allChunks []string

	for i, text := range texts {
		chunks := chunkText(text, maxCharsPerChunk)
		plan := textPlan{
			origIdx:    i,
			chunkStart: len(allChunks),
			chunkEnd:   len(allChunks) + len(chunks),
		}
		allChunks = append(allChunks, chunks...)
		plans = append(plans, plan)
	}

	if len(allChunks) == 0 {
		return make([][]float32, len(texts)), nil
	}

	// Batch-embed all chunks. The API supports up to 50 inputs per request.
	// For very large sets, we'd need to paginate, but in practice: our typical
	// call is []string{content} — a single text chunked into maybe 2-8 pieces.
	// Even if called with multiple texts, total chunks rarely exceed 50.
	chunkVecs, err := e.embedBatch(ctx, allChunks)
	if err != nil {
		return nil, err
	}

	// Reassemble: for each original text, mean-pool its chunk vectors.
	results := make([][]float32, len(texts))
	for _, plan := range plans {
		chunksForText := chunkVecs[plan.chunkStart:plan.chunkEnd]
		if len(chunksForText) == 1 {
			results[plan.origIdx] = chunksForText[0]
		} else {
			results[plan.origIdx] = meanPool(chunksForText)
		}
	}

	return results, nil
}

// EmbedQuery converts a single query text to a vector.
// Queries are typically short — no chunking needed.
func (e *HunyuanEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.embedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("hunyuan embed: empty response")
	}
	return vecs[0], nil
}

// embedBatch sends a batch of texts to the embedding API in a single request.
func (e *HunyuanEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
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
		return nil, fmt.Errorf("hunyuan embed API: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("hunyuan embed: empty response data")
	}

	// Convert: sort by index (API may reorder), convert float64 → float32.
	vecs := make([][]float32, len(resp.Data))
	for _, emb := range resp.Data {
		if int(emb.Index) >= len(vecs) {
			return nil, fmt.Errorf("hunyuan embed: unexpected index %d (expected < %d)", emb.Index, len(vecs))
		}
		vecs[emb.Index] = float64ToFloat32(emb.Embedding)
	}

	return vecs, nil
}

// chunkText splits text into chunks that stay within maxChars characters.
//
// Strategy:
//  1. Split by paragraph boundaries (double newline) first.
//  2. Accumulate paragraphs into chunks, starting a new chunk when adding
//     the next paragraph would exceed maxChars.
//  3. If a single paragraph exceeds maxChars, split it at sentence
//     boundaries (。！？. ! ?) or, as a last resort, at fixed intervals.
func chunkText(text string, maxChars int) []string {
	if text == "" {
		return []string{""}
	}
	if utf8.RuneCountInString(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	paragraphs := strings.Split(text, "\n\n")

	var current strings.Builder
	for _, para := range paragraphs {
		paraLen := utf8.RuneCountInString(para)

		// Empty paragraph — preserve it as a separator in the current chunk.
		if paraLen == 0 {
			if current.Len() > 0 {
				current.WriteString("\n\n")
			}
			continue
		}

		// If adding this paragraph would exceed the limit, flush the current chunk.
		if current.Len() > 0 && utf8.RuneCountInString(current.String())+paraLen+2 > maxChars {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		// If the paragraph itself exceeds maxChars, split it further.
		if paraLen > maxChars {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			subChunks := splitLongParagraph(para, maxChars)
			chunks = append(chunks, subChunks...)
			continue
		}

		// Add paragraph to the current chunk.
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// splitLongParagraph splits a single paragraph that exceeds maxChars into
// smaller pieces at sentence boundaries. Falls back to fixed-interval
// splitting if no sentence boundaries are found.
func splitLongParagraph(text string, maxChars int) []string {
	sentences := splitSentences(text)

	var chunks []string
	var current strings.Builder

	for _, sent := range sentences {
		sentLen := utf8.RuneCountInString(sent)

		// If a single sentence exceeds maxChars, split at fixed intervals.
		if sentLen > maxChars {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			fixedChunks := splitFixed(sent, maxChars)
			chunks = append(chunks, fixedChunks...)
			continue
		}

		// If adding this sentence would exceed the limit, flush.
		if current.Len() > 0 && utf8.RuneCountInString(current.String())+sentLen+1 > maxChars {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sent)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// splitSentences splits text at sentence boundaries for both Chinese and English.
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i, r := range runes {
		current.WriteRune(r)

		isEnd := false
		switch r {
		case '。', '！', '？', '\n':
			isEnd = true
		case '.', '!', '?':
			// English sentence endings: require whitespace, newline, or end after.
			if i+1 >= len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' {
				isEnd = true
			}
		}

		if isEnd && current.Len() > 0 {
			sentences = append(sentences, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}

	if current.Len() > 0 {
		s := strings.TrimSpace(current.String())
		if s != "" {
			sentences = append(sentences, s)
		}
	}

	if len(sentences) == 0 {
		return []string{text}
	}
	return sentences
}

// splitFixed splits text at fixed rune intervals. Used as a last resort when
// no sentence boundaries are found.
func splitFixed(text string, maxChars int) []string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return []string{text}
	}

	var chunks []string
	for i := 0; i < len(runes); i += maxChars {
		end := i + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

// meanPool averages multiple same-length vectors element-wise.
func meanPool(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	if len(vecs) == 1 {
		return vecs[0]
	}

	dim := len(vecs[0])
	result := make([]float32, dim)
	for _, vec := range vecs {
		for i, v := range vec {
			result[i] += v
		}
	}

	n := float32(len(vecs))
	for i := range result {
		result[i] /= n
	}

	return result
}

// float64ToFloat32 converts a float64 slice to float32.
func float64ToFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}
