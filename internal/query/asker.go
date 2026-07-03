package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// AskOptions controls AI question-answering behavior.
type AskOptions struct {
	// TopN is the number of top search results to use as context.
	// Default: 5.
	TopN int

	// Save controls whether to write the Q&A result back as a wiki page.
	Save bool

	// NoStream disables streaming output.
	NoStream bool
}

// AskResult is the final result of an AI Q&A request.
type AskResult struct {
	// Answer is the complete synthesized answer text.
	Answer string

	// Sources lists the wiki pages referenced in the answer.
	Sources []Source
}

// Source represents a wiki page used as context for an answer.
type Source struct {
	Title   string
	Path    string
	Snippet string
}

// AskChunk is a streaming fragment of an answer in progress.
type AskChunk struct {
	Content string // delta text; empty when Done is true and no error
	Done    bool   // true when this is the last chunk
	Error   error  // non-nil on error
}

// Ask sends a question to the LLM with relevant wiki pages as context and
// returns the synthesized answer with source citations.
func (e *Engine) Ask(ctx context.Context, question string, opts *AskOptions) (*AskResult, error) {
	topN := 5
	save := false
	if opts != nil {
		if opts.TopN > 0 {
			topN = opts.TopN
		}
		save = opts.Save
	}

	// 1. Search for relevant pages
	sources, err := e.retrieveContext(ctx, question, topN)
	if err != nil {
		return nil, fmt.Errorf("retrieving context: %w", err)
	}

	if len(sources) == 0 {
		return &AskResult{
			Answer:  "I couldn't find any relevant pages in the wiki to answer this question.",
			Sources: nil,
		}, nil
	}

	// 2. Build prompt with context
	messages := e.buildAskMessages(question, sources)

	// 3. Call LLM
	resp, err := e.llm.Chat(ctx, messages, &llm.ChatOptions{
		Temperature: e.llmCfg.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM chat: %w", err)
	}

	result := &AskResult{
		Answer:  resp.Content,
		Sources: sources,
	}

	// 4. Optionally save to wiki
	if save {
		if err := e.saveToWiki(question, result); err != nil {
			return result, fmt.Errorf("saving to wiki: %w", err)
		}
	}

	return result, nil
}

// AskStream is like Ask but streams the answer as it is generated.
//
// The returned channel emits chunks of the answer text. The final chunk has
// Done=true and includes the source list in its Error field as a nil error
// (the caller should check Done, not Error, for the end of stream).
//
// Usage:
//
//	ch, err := engine.AskStream(ctx, "What is X?", nil)
//	for chunk := range ch {
//	    if chunk.Error != nil { ... }
//	    if chunk.Done { break }
//	    fmt.Print(chunk.Content)
//	}
func (e *Engine) AskStream(ctx context.Context, question string, opts *AskOptions) (<-chan AskChunk, error) {
	topN := 5
	save := false
	if opts != nil {
		if opts.TopN > 0 {
			topN = opts.TopN
		}
		save = opts.Save
	}

	// 1. Search for relevant pages
	sources, err := e.retrieveContext(ctx, question, topN)
	if err != nil {
		return nil, fmt.Errorf("retrieving context: %w", err)
	}

	if len(sources) == 0 {
		ch := make(chan AskChunk, 1)
		ch <- AskChunk{
			Content: "I couldn't find any relevant pages in the wiki to answer this question.\n",
			Done:    true,
		}
		close(ch)
		return ch, nil
	}

	// 2. Build prompt with context
	messages := e.buildAskMessages(question, sources)

	// 3. Start streaming LLM call
	llmCh, err := e.llm.ChatStream(ctx, messages, &llm.ChatOptions{
		Temperature: e.llmCfg.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM chat stream: %w", err)
	}

	// 4. Adapt the LLM chunk stream to AskChunk stream, appending sources on done
	askCh := make(chan AskChunk, 10)
	go func() {
		defer close(askCh)
		var fullAnswer strings.Builder
		for chunk := range llmCh {
			if chunk.Error != nil {
				askCh <- AskChunk{Error: chunk.Error}
				return
			}
			if chunk.Done {
				break
			}
			fullAnswer.WriteString(chunk.Content)
			askCh <- AskChunk{Content: chunk.Content}
		}

		// Optionally save to wiki with the complete answer
		if save {
			result := &AskResult{
				Answer:  fullAnswer.String(),
				Sources: sources,
			}
			if err := e.saveToWiki(question, result); err != nil {
				askCh <- AskChunk{Error: fmt.Errorf("saving to wiki: %w", err)}
				return
			}
		}

		// Signal done
		askCh <- AskChunk{Done: true}
	}()

	return askCh, nil
}

// retrieveContext searches the wiki for pages relevant to the question.
//
// When an EmbeddingProvider is configured, it uses hybrid retrieval:
// FTS5 (keyword) + vector similarity, fused with Reciprocal Rank Fusion.
// Otherwise falls back to FTS5-only with AND→OR fallback.
func (e *Engine) retrieveContext(ctx context.Context, question string, topN int) ([]Source, error) {
	var results []wiki.SearchResult
	var err error

	if e.embedder != nil {
		results, err = e.hybridSearch(ctx, question, topN)
	} else {
		results, err = e.wiki.Index().SearchWithSnippets(question, topN)
	}
	if err != nil {
		return nil, err
	}

	sources := make([]Source, 0, len(results))
	for _, r := range results {
		// Read the full page content for context
		page, err := e.wiki.ReadByPath(r.Path)
		if err != nil {
			// Skip unreadable pages (don't fail the whole query)
			continue
		}

		sources = append(sources, Source{
			Title:   r.Title,
			Path:    r.Path,
			Snippet: stripTags(r.Snippet),
		})

		_ = page // page fetched successfully, used in buildAskMessages
	}

	return sources, nil
}

// hybridSearch performs FTS5 keyword search and vector similarity search,
// then fuses the two ranked lists with Reciprocal Rank Fusion.
//
// If either search fails, the other is used alone. This ensures graceful
// degradation when embeddings are unavailable for some pages.
func (e *Engine) hybridSearch(ctx context.Context, question string, topN int) ([]wiki.SearchResult, error) {
	// Double the limit for each sub-search since RRF will deduplicate and re-rank.
	subLimit := topN * 2

	// FTS5 keyword search
	ftsResults, ftsErr := e.wiki.Index().SearchWithSnippets(question, subLimit)

	// Vector similarity search
	queryVec, embErr := e.embedder.EmbedQuery(ctx, question)
	var vecResults []wiki.SearchResult
	if embErr == nil {
		vecResults, _ = e.wiki.Index().SearchByVector(queryVec, subLimit)
	}

	// If both failed, return what we have
	if ftsErr != nil && (embErr != nil || len(vecResults) == 0) {
		if ftsErr != nil {
			return nil, ftsErr
		}
		return nil, embErr
	}

	// If only one side succeeded, use it alone
	if ftsErr != nil {
		return vecResults[:min(topN, len(vecResults))], nil
	}
	if embErr != nil || len(vecResults) == 0 {
		return ftsResults[:min(topN, len(ftsResults))], nil
	}

	// Fuse with RRF
	return rrfFuse(ftsResults, vecResults, topN), nil
}

// rrfFuse fuses two ranked result lists using Reciprocal Rank Fusion (k=60).
// Results are deduplicated by path and sorted by descending RRF score.
func rrfFuse(ftsResults, vecResults []wiki.SearchResult, limit int) []wiki.SearchResult {
	const k = 60

	score := make(map[string]float64)
	result := make(map[string]wiki.SearchResult)

	for i, r := range ftsResults {
		score[r.Path] += 1.0 / (k + float64(i+1))
		if _, exists := result[r.Path]; !exists {
			result[r.Path] = r
		}
	}
	for i, r := range vecResults {
		score[r.Path] += 1.0 / (k + float64(i+1))
		if _, exists := result[r.Path]; !exists {
			result[r.Path] = r
		}
	}

	// Sort by descending score
	type scored struct {
		wiki.SearchResult
		score float64
	}
	var list []scored
	for path, r := range result {
		list = append(list, scored{r, score[path]})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].score > list[j].score
	})

	if limit > len(list) {
		limit = len(list)
	}
	out := make([]wiki.SearchResult, limit)
	for i := 0; i < limit; i++ {
		out[i] = list[i].SearchResult
	}
	return out
}

// buildAskMessages constructs the LLM prompt with context and question.
func (e *Engine) buildAskMessages(question string, sources []Source) []llm.Message {
	var contextBuilder strings.Builder
	contextBuilder.WriteString("## Relevant Wiki Pages\n\n")
	for i, src := range sources {
		// Read full content for each source
		page, err := e.wiki.ReadByPath(src.Path)
		if err != nil {
			continue
		}

		fmt.Fprintf(&contextBuilder, "### [%d] %s (%s)\n\n", i+1, src.Title, src.Path)
		// Truncate very long pages to avoid exceeding context window
		content := page.Content
		if len(content) > 4000 {
			content = content[:4000] + "\n\n... (truncated)"
		}
		contextBuilder.WriteString(content)
		contextBuilder.WriteString("\n\n---\n\n")
	}

	systemPrompt := fmt.Sprintf(`You are a knowledgeable research assistant answering questions based on the user's personal wiki.

Below are relevant wiki pages that may contain the answer. Use ONLY the provided context to answer the question. If the context doesn't contain enough information, say so honestly — do not fabricate facts.

Cite your sources inline using the page title in [[double brackets]], e.g. "According to [[Entity Name]], ...".

%s`, contextBuilder.String())

	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: question},
	}
}

// saveToWiki saves a Q&A exchange as a synthesis page.
func (e *Engine) saveToWiki(question string, result *AskResult) error {
	title := fmt.Sprintf("Q&A: %s", truncate(question, 60))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", title))
	b.WriteString(fmt.Sprintf("**Question**: %s\n\n", question))
	b.WriteString("## Answer\n\n")
	b.WriteString(result.Answer)
	b.WriteString("\n\n## Sources\n\n")
	for _, src := range result.Sources {
		b.WriteString(fmt.Sprintf("- [[%s]] (%s)\n", src.Title, src.Path))
	}

	// Check if page already exists and update, or create new
	existing, err := e.wiki.Read(title, wiki.PageTypeSynthesis)
	if err != nil {
		// Page doesn't exist — create it
		_, err = e.wiki.Create(title, wiki.PageTypeSynthesis, b.String())
		return err
	}
	// Page exists — update it
	_, err = e.wiki.Update(existing.Title, wiki.PageTypeSynthesis, b.String())
	return err
}

// truncate shortens a string to maxLen characters, appending "..." if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// stripTags removes <b> and </b> tags from a snippet for plain-text display.
func stripTags(s string) string {
	s = strings.ReplaceAll(s, "<b>", "")
	s = strings.ReplaceAll(s, "</b>", "")
	return s
}
