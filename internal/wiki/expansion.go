package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/llm"
)

// SearchEffort controls the level of query rewriting/expansion applied
// before vector search. Higher effort = better recall at the cost of latency.
type SearchEffort string

const (
	// SearchEffortFast is the baseline: no query expansion, current pipeline.
	//
	// Pipeline:
	// Query → embed(query) → vector search → FTS boost → MMR → Rerank → topN
	SearchEffortFast SearchEffort = "fast"
	// SearchEffortBalanced uses Query Expansion: LLM generates 2-3 query
	// variants, each searched independently, results merged via RRF.
	//
	// Pipeline:
	// Query → LLM hypo doc → embed(hypo) → vector search → FTS boost → MMR → Rerank → topN
	SearchEffortBalanced SearchEffort = "balanced"
	// SearchEffortThorough uses HyDE (Hypothetical Document Embeddings):
	// LLM generates a hypothetical answer passage, then its embedding
	// replaces the original query embedding for vector search.
	//
	// Pipeline:
	// Query → LLM expand → [q1,q2,q3] → 3×vector search → RRF merge → FTS boost → MMR → Rerank → topN
	SearchEffortThorough SearchEffort = "thorough"
)

// expandQueries uses the LLM to rewrite the original query into 2-3
// alternative formulations that cover different angles and terminology.
// This helps bridge the vocabulary gap between how users ask questions
// and how documents are written.
//
// On any failure (LLM unavailable, network error, unparseable response),
// the original query is returned as a single-element slice — expansion is
// best-effort, never a hard dependency.
func expandQueries(ctx context.Context, provider llm.LLMProvider, query string) ([]string, error) {
	if provider == nil {
		return []string{query}, nil
	}

	messages := buildExpansionPrompt(query)

	resp, err := provider.Chat(ctx, messages, &llm.ChatOptions{
		Temperature: 0,
		MaxTokens:   512,
	})
	if err != nil || resp == nil || resp.Content == "" {
		return []string{query}, nil
	}

	variants, err := parseExpansionResponse(resp.Content)
	if err != nil || len(variants) == 0 {
		return []string{query}, nil
	}

	// Prepend the original query to ensure it's always part of the search.
	// Deduplicate while preserving order.
	seen := map[string]bool{query: true}
	result := []string{query}
	for _, v := range variants {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		result = append(result, v)
	}

	return result, nil
}

// generateHypotheticalDoc uses the LLM to write a short passage that answers
// the query. The passage's embedding is then used for vector search instead
// of the original query embedding — this bridges the distribution gap
// between questions (short, interrogative) and documents (long, declarative).
//
// On any failure, an empty string is returned and the caller should fall
// back to using the original query embedding.
func generateHypotheticalDoc(ctx context.Context, provider llm.LLMProvider, query string) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("no LLM provider available")
	}

	messages := buildHyDEPrompt(query)

	resp, err := provider.Chat(ctx, messages, &llm.ChatOptions{
		Temperature: 0,
		MaxTokens:   1024,
	})
	if err != nil || resp == nil || resp.Content == "" {
		return "", fmt.Errorf("HyDE generation failed: %w", err)
	}

	doc := strings.TrimSpace(resp.Content)
	if doc == "" {
		return "", fmt.Errorf("HyDE generated empty document")
	}

	return doc, nil
}

// buildExpansionPrompt constructs the system and user messages for query expansion.
// The LLM is asked to generate 2-3 alternative query formulations and output
// them as a JSON array.
func buildExpansionPrompt(query string) []llm.Message {
	systemPrompt := `You are a search query rewriter. Given a user's search query, generate
2-3 alternative formulations that express the same information need from
different angles.

Guidelines:
- Use different terminology and synonyms where appropriate
- One variant should use more technical/academic language
- One variant should use broader or related terms
- Keep each variant concise (one sentence or phrase)
- Output your response as a JSON array of strings

Example:
Query: "how to deploy a web app"
Output: ["web application deployment guide", "steps for deploying a website to production", "cloud hosting setup for web services"]

Output ONLY the JSON array with no additional text.`

	userPrompt := fmt.Sprintf("Query: %s\n\nGenerate alternative search queries:", query)

	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// buildHyDEPrompt constructs the system and user messages for HyDE
// (Hypothetical Document Embeddings). The LLM is asked to write a short
// passage that answers the query, as if it were an encyclopedia entry.
func buildHyDEPrompt(query string) []llm.Message {
	systemPrompt := `You are a knowledgeable encyclopedia writer. Given a question,
write a short passage (2-4 paragraphs) that answers it, as if you were
writing an encyclopedia entry or a textbook section. Include key facts,
terminology, and concepts that would appear in such a document.

Write in an informative, declarative style. Use precise technical terms.
Do NOT begin with phrases like "This passage answers..." or "The answer is..."
— just write the passage directly as if it were a real document.`

	userPrompt := fmt.Sprintf("Question: %s\n\nWrite a short encyclopedic passage that answers this question:", query)

	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// parseExpansionResponse extracts the query variants from the LLM response.
// It handles markdown code fences and leading/trailing commentary.
// The expected format is: ["variant1", "variant2", "variant3"]
func parseExpansionResponse(response string) ([]string, error) {
	cleaned := response
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	// Extract JSON array: find first '[' and last ']'
	start := strings.Index(cleaned, "[")
	end := strings.LastIndex(cleaned, "]")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found in response")
	}
	jsonStr := cleaned[start : end+1]

	var variants []string
	if err := json.Unmarshal([]byte(jsonStr), &variants); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	return variants, nil
}

// rrfFuseMultiQuery merges multiple sets of vector search results (one per
// expanded query) into a single ranked pool using Reciprocal Rank Fusion.
// Results appearing in multiple query result sets receive higher scores.
func rrfFuseMultiQuery(allResults [][]scoredResult) []scoredResult {
	const k = 60

	rrfScore := make(map[string]float64)
	result := make(map[string]scoredResult)

	for _, results := range allResults {
		for i, r := range results {
			rrfScore[r.Path] += 1.0 / (k + float64(i+1))
			if _, exists := result[r.Path]; !exists {
				result[r.Path] = r
			}
		}
	}

	// Build sorted list
	out := make([]scoredResult, 0, len(result))
	for path, r := range result {
		r.score = rrfScore[path]
		out = append(out, r)
	}

	// Sort by descending RRF score
	sort.Slice(out, func(i, j int) bool {
		return out[i].score > out[j].score
	})
	return out
}

// multiQuerySearch performs vector search for each expanded query variant,
// merges the results via RRF, then continues the standard pipeline (FTS
// boosting, MMR diversity, LLM rerank, truncation).
func (m *Manager) multiQuerySearch(ctx context.Context, queries []string, topN int) ([]SearchResult, error) {
	const recallSize = 200

	// Vector search for each variant, collecting scored results.
	var allResults [][]scoredResult
	for i, q := range queries {
		queryVec, err := m.embedder.EmbedQuery(ctx, q)
		if err != nil {
			if m.tracer != nil {
				m.tracer.Begin("vector", "variant", i)
				m.tracer.Error(err)
				m.tracer.End()
			}
			continue
		}

		results, err := m.index.searchByVectorWithMeta(queryVec, recallSize)
		if err != nil {
			continue
		}

		if m.tracer != nil {
			m.tracer.Begin("vector", "variant", i, "pool", recallSize)
			m.tracer.End("results", len(results), "query", truncateForTrace(q, 60))
		}

		if len(results) > 0 {
			allResults = append(allResults, results)
		}
	}

	if len(allResults) == 0 {
		// All variants failed — fall back to FTS.
		return m.ftsWithFallback(queries[0], topN)
	}

	// RRF-merge all variant result sets.
	scoredResults := rrfFuseMultiQuery(allResults)

	if m.tracer != nil {
		m.tracer.Begin("rrf", "queries", len(queries))
		m.tracer.End("fused", len(scoredResults))
	}

	// FTS boosting (use the original query for keyword matching).
	ftsBoosted := false
	if andQuery := toFTS5AndQuery(queries[0]); andQuery != "" {
		ftsResults, ftsErr := m.index.searchWithSnippets(andQuery, 5)
		if ftsErr == nil && len(ftsResults) > 0 {
			scoredResults = rrfFuseFull(ftsResults, scoredResults)
			ftsBoosted = true
			if m.tracer != nil {
				m.tracer.Begin("fts", "type", "AND")
				m.tracer.End("results", len(ftsResults), "docs", docList(ftsResults[:min(3, len(ftsResults))]))
			}
		}
	}
	if !ftsBoosted {
		if orQuery := toFTS5OrQuery(queries[0]); orQuery != "" {
			orResults, err := m.index.searchWithSnippets(orQuery, 5)
			if err == nil && len(orResults) > 0 {
				scoredResults = rrfFuseFull(orResults, scoredResults)
				if m.tracer != nil {
					m.tracer.Begin("fts", "type", "OR")
					m.tracer.End("results", len(orResults), "docs", docList(orResults[:min(3, len(orResults))]))
				}
			}
		}
	}

	// MMR diversity — use the first variant's embedding.
	queryVec, _ := m.embedder.EmbedQuery(ctx, queries[0])
	const mmrTarget = 50
	mmrN := mmrTarget
	if mmrN > len(scoredResults) {
		mmrN = len(scoredResults)
	}
	diverse := mmrDiversify(queryVec, scoredResults, 0.5, mmrN)

	if m.tracer != nil {
		m.tracer.Begin("mmr", "lambda", 0.5, "target", mmrTarget)
		m.tracer.End("selected", len(diverse), "docs", docList(diverse[:min(10, len(diverse))]))
	}

	// LLM rerank.
	if m.llmProvider != nil && len(diverse) > topN {
		beforeRerank := len(diverse)
		diverse = m.rerankWithLLM(ctx, queries[0], diverse)
		if m.tracer != nil {
			m.tracer.Begin("rerank")
			m.tracer.End("candidates", fmt.Sprintf("%d→%d", beforeRerank, len(diverse)),
				"docs", docList(diverse[:min(5, len(diverse))]))
		}
	}

	// Truncate to topN.
	if topN < len(diverse) {
		if m.tracer != nil {
			m.tracer.Begin("final")
			m.tracer.End("returned", topN, "docs", docList(diverse[:topN]))
		}
		return diverse[:topN], nil
	}
	if m.tracer != nil {
		m.tracer.Begin("final")
		m.tracer.End("returned", len(diverse), "docs", docList(diverse[:min(5, len(diverse))]))
	}
	return diverse, nil
}

// truncateForTrace shortens a string for trace output.
func truncateForTrace(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
