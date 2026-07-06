package wiki

import (
	"context"
	"errors"
	"testing"

	"github.com/hitzhangjie/ruminate/internal/llm"
)

// mockLLMProvider is reused from rerank_test.go in this package.

// --- expandQueries tests (with mock LLM provider) ---

func TestExpandQueries(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		provider := &mockLLMProvider{
			chatFunc: func(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
				return &llm.ChatResponse{
					Content: `["Go garbage collection transparent huge pages", "Go runtime THP memory allocation", "golang GC huge page support"]`,
				}, nil
			},
		}

		variants, err := expandQueries(context.Background(), provider, "Go GC 如何适应透明巨页")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should include original query + at least 1 variant
		if len(variants) < 2 {
			t.Fatalf("expected at least 2 variants (original + expanded), got %d: %v", len(variants), variants)
		}

		// Original query should be first
		if variants[0] != "Go GC 如何适应透明巨页" {
			t.Errorf("expected original query first, got %q", variants[0])
		}
	})

	t.Run("Deduplicate", func(t *testing.T) {
		provider := &mockLLMProvider{
			chatFunc: func(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
				// Returns a duplicate of the original query
				return &llm.ChatResponse{
					Content: `["Go GC 如何适应透明巨页", "another variant"]`,
				}, nil
			},
		}

		variants, err := expandQueries(context.Background(), provider, "Go GC 如何适应透明巨页")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Duplicate should be removed
		for i, v := range variants {
			for j := i + 1; j < len(variants); j++ {
				if v == variants[j] {
					t.Errorf("duplicate variant at positions %d and %d: %q", i, j, v)
				}
			}
		}
	})

	t.Run("NilProvider", func(t *testing.T) {
		variants, err := expandQueries(context.Background(), nil, "test query")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(variants) != 1 || variants[0] != "test query" {
			t.Errorf("expected original query only with nil provider, got %v", variants)
		}
	})

	t.Run("LLMError", func(t *testing.T) {
		provider := &mockLLMProvider{
			chatFunc: func(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
				return nil, errors.New("network error")
			},
		}

		variants, err := expandQueries(context.Background(), provider, "test query")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should gracefully fall back to original query
		if len(variants) != 1 || variants[0] != "test query" {
			t.Errorf("expected fallback to original query, got %v", variants)
		}
	})

	t.Run("EmptyResponse", func(t *testing.T) {
		provider := &mockLLMProvider{
			chatFunc: func(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
				return &llm.ChatResponse{Content: ""}, nil
			},
		}

		variants, err := expandQueries(context.Background(), provider, "test query")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(variants) != 1 || variants[0] != "test query" {
			t.Errorf("expected fallback to original query on empty response, got %v", variants)
		}
	})
}

// --- generateHypotheticalDoc tests (with mock LLM provider) ---

func TestGenerateHypotheticalDoc(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		provider := &mockLLMProvider{
			chatFunc: func(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
				return &llm.ChatResponse{
					Content: `Go's garbage collector interacts with transparent huge pages (THP) through the runtime's memory allocator. When the operating system enables THP, the Go runtime must handle larger memory pages, which can affect the efficiency of garbage collection sweeping and object allocation patterns.`,
				}, nil
			},
		}

		doc, err := generateHypotheticalDoc(context.Background(), provider, "Go GC 如何适应透明巨页")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if doc == "" {
			t.Error("expected non-empty hypothetical document")
		}
	})

	t.Run("NilProvider", func(t *testing.T) {
		_, err := generateHypotheticalDoc(context.Background(), nil, "test query")
		if err == nil {
			t.Error("expected error with nil provider")
		}
	})

	t.Run("LLMError", func(t *testing.T) {
		provider := &mockLLMProvider{
			chatFunc: func(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
				return nil, errors.New("network error")
			},
		}

		_, err := generateHypotheticalDoc(context.Background(), provider, "test query")
		if err == nil {
			t.Error("expected error on LLM failure")
		}
	})

	t.Run("EmptyResponse", func(t *testing.T) {
		provider := &mockLLMProvider{
			chatFunc: func(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
				return &llm.ChatResponse{Content: ""}, nil
			},
		}

		_, err := generateHypotheticalDoc(context.Background(), provider, "test query")
		if err == nil {
			t.Error("expected error on empty response")
		}
	})
}

// --- parseExpansionResponse tests (pure function, no dependencies) ---

func TestParseExpansionResponse(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		variants, err := parseExpansionResponse(`["variant one", "variant two", "variant three"]`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(variants) != 3 {
			t.Errorf("expected 3 variants, got %d", len(variants))
		}
	})

	t.Run("WithMarkdownFence", func(t *testing.T) {
		variants, err := parseExpansionResponse("```json\n[\"a\", \"b\"]\n```")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(variants) != 2 {
			t.Errorf("expected 2 variants, got %d", len(variants))
		}
	})

	t.Run("WithCommentary", func(t *testing.T) {
		variants, err := parseExpansionResponse("Here are some variants:\n[\"v1\", \"v2\"]\nHope that helps!")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(variants) != 2 {
			t.Errorf("expected 2 variants, got %d", len(variants))
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		_, err := parseExpansionResponse("not json at all")
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("EmptyArray", func(t *testing.T) {
		_, err := parseExpansionResponse("[]")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Empty array is valid JSON, caller should handle it
	})
}

// --- rrfFuseMultiQuery tests ---

func TestRRFFuseMultiQuery(t *testing.T) {
	// Simulate 3 query variants returning overlapping results
	r1 := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Title: "Doc A", Path: "a.md"}}, score: 0.9},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Title: "Doc B", Path: "b.md"}}, score: 0.8},
	}
	r2 := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Title: "Doc A", Path: "a.md"}}, score: 0.85},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Title: "Doc C", Path: "c.md"}}, score: 0.7},
	}
	r3 := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Title: "Doc B", Path: "b.md"}}, score: 0.75},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Title: "Doc D", Path: "d.md"}}, score: 0.6},
	}

	merged := rrfFuseMultiQuery([][]scoredResult{r1, r2, r3})

	// Should contain all unique docs: A, B, C, D
	if len(merged) != 4 {
		t.Errorf("expected 4 unique docs, got %d", len(merged))
	}

	// Doc A appears in r1 and r2 → should rank highest
	if merged[0].Path != "a.md" {
		t.Errorf("expected Doc A first (appears in 2 queries), got %s", merged[0].Path)
	}
}
