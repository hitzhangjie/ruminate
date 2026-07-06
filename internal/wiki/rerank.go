package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hitzhangjie/ruminate/internal/llm"
)

// maxPreviewChars is the maximum number of characters to extract from a page
// as a content preview for the rerank prompt. Keeping this modest ensures the
// total prompt size stays manageable even with 50 candidates.
const maxPreviewChars = 300

// rerankWithLLM uses the LLM to perform listwise relevance ranking on the
// candidates. It issues a single Chat call asking the model to reorder the
// documents by relevance to the query, then returns the reordered slice.
//
// Candidates that the LLM omits from its ranking are considered irrelevant
// and are discarded — the returned slice may be shorter than the input.
//
// On any failure (LLM unavailable, network error, unparseable response,
// empty ranking), the original candidate order is returned unchanged —
// rerank is a best-effort precision improvement, never a hard dependency.
func (m *Manager) rerankWithLLM(ctx context.Context, query string, candidates []SearchResult) []SearchResult {
	if m.llmProvider == nil || len(candidates) <= 1 {
		return candidates
	}

	// Build the rerank prompt
	messages, err := buildRerankPrompt(query, candidates, m)
	if err != nil {
		return candidates
	}

	// Call LLM with temperature=0 for deterministic scoring
	resp, err := m.llmProvider.Chat(ctx, messages, &llm.ChatOptions{
		Temperature: 0,
		MaxTokens:   2048,
	})
	if err != nil || resp == nil || resp.Content == "" {
		return candidates
	}

	// Parse the ranked ID order from the LLM response
	rankedIDs, err := parseRerankResponse(resp.Content, len(candidates))
	if err != nil {
		return candidates
	}

	// Reorder candidates according to the LLM ranking.
	// Candidates the LLM omits are considered irrelevant and discarded.
	reordered := make([]SearchResult, 0, len(rankedIDs))
	placed := make(map[int]bool, len(rankedIDs))

	for _, id := range rankedIDs {
		// rankedIDs from parseRerankResponse are 0-based indices after conversion.
		// The LLM outputs 1-based IDs; parseRerankResponse converts to 0-based.
		if id >= 0 && id < len(candidates) && !placed[id] {
			reordered = append(reordered, candidates[id])
			placed[id] = true
		}
	}

	// Update Rank to reflect the new 1-based position so callers
	// get a meaningful relevance signal.
	for i := range reordered {
		reordered[i].Rank = float64(i + 1)
	}

	return reordered
}

// getContentPreview reads a page from disk and returns the first ~maxPreviewChars
// characters of useful content (stripping heading markers and breaking at a
// paragraph boundary when possible).
func (m *Manager) getContentPreview(path string) string {
	page, err := m.ReadByPath(path)
	if err != nil || page == nil {
		return ""
	}

	content := page.Content
	// Strip leading "# " heading markers (up to level 3)
	lines := strings.Split(content, "\n")
	var bodyLines []string
	foundContent := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip leading headings and blank lines
		if !foundContent && (strings.HasPrefix(trimmed, "#") || trimmed == "") {
			continue
		}
		foundContent = true
		bodyLines = append(bodyLines, line)
	}

	content = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	if content == "" {
		// Fall back to the original content if stripping removed everything
		content = strings.TrimSpace(page.Content)
	}

	// Truncate to maxPreviewChars, trying to break at a paragraph boundary
	if utf8.RuneCountInString(content) <= maxPreviewChars {
		return content
	}

	truncated := content
	// Find the last paragraph break (\n\n) within the limit
	if idx := strings.LastIndex(truncated[:maxPreviewChars], "\n\n"); idx > maxPreviewChars/2 {
		return truncated[:idx]
	}
	// Find the last sentence boundary (。or . followed by space/newline) within the limit
	if idx := strings.LastIndex(truncated[:maxPreviewChars], "。"); idx > maxPreviewChars/2 {
		return truncated[:idx+len("。")]
	}
	// Hard truncate — still break at a rune boundary
	runes := []rune(truncated)
	if len(runes) > maxPreviewChars {
		return string(runes[:maxPreviewChars])
	}
	return truncated
}

// buildRerankPrompt constructs the system and user messages for the LLM rerank
// call. It numbers each candidate and includes the query, title, and content
// preview. The LLM is instructed to output JSON with the ranked IDs.
func buildRerankPrompt(query string, candidates []SearchResult, m *Manager) ([]llm.Message, error) {
	var docsBuilder strings.Builder
	for i, c := range candidates {
		preview := m.getContentPreview(c.Path)
		fmt.Fprintf(&docsBuilder, "[%d] Title: %s\n", i+1, c.Title)
		if preview != "" {
			fmt.Fprintf(&docsBuilder, "Preview: %s\n", preview)
		}
		if i < len(candidates)-1 {
			docsBuilder.WriteString("---\n")
		}
	}

	systemPrompt := `You are a search relevance judge. Given a user query and a numbered list of documents,
rank the documents by how relevant they are to the query.

Consider which documents best answer the query, contain the most relevant
information, and are most useful to the user.

IMPORTANT: Only include documents that are genuinely relevant to the query.
Omit any document that is not relevant enough to help answer the query.
If no documents are relevant, return an empty list.

Output your ranking as a JSON object with a single key "ranked_ids" containing
an array of document numbers in order of relevance (most relevant first).

Example: {"ranked_ids": [3, 1, 5, 2, 4]}

Output ONLY the JSON object with no additional text.`

	userPrompt := fmt.Sprintf("Query: %s\n\nDocuments:\n%s\n\nRank the documents by relevance to the query.", query, docsBuilder.String())

	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, nil
}

// parseRerankResponse extracts the ranked ID list from the LLM's response.
// It handles common output variations: markdown code fences, leading/trailing
// commentary, and whitespace. Returns 0-based indices.
//
// The expected format is: {"ranked_ids": [3, 1, 5, 2, 4]}
// where the numbers are 1-based document IDs (as shown to the LLM).
func parseRerankResponse(response string, numCandidates int) ([]int, error) {
	cleaned := response

	// Strip markdown code fences if present
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	// Extract JSON object: find first '{' and last '}'
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	jsonStr := cleaned[start : end+1]

	// Parse the JSON
	var result struct {
		RankedIDs []int `json:"ranked_ids"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	if len(result.RankedIDs) == 0 {
		return nil, fmt.Errorf("empty ranked_ids")
	}

	// Validate and convert 1-based IDs to 0-based indices
	seen := make(map[int]bool, len(result.RankedIDs))
	indices := make([]int, 0, len(result.RankedIDs))
	for _, id := range result.RankedIDs {
		if id < 1 || id > numCandidates {
			continue // skip out-of-range IDs
		}
		idx := id - 1 // convert to 0-based
		if seen[idx] {
			continue // skip duplicates
		}
		seen[idx] = true
		indices = append(indices, idx)
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("no valid IDs after validation")
	}

	return indices, nil
}
