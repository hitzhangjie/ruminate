package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// AskOptions controls AI question-answering behavior.
type AskOptions struct {
	// TopN is the number of top search results to use as context.
	// Default: DefaultTopN (20).
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
	Content string   // delta text; empty when Done is true and no error
	Done    bool     // true when this is the last chunk
	Error   error    // non-nil on error
	Sources []Source // populated on the final chunk (Done=true, Error=nil)
}

const DefaultTopN = 20

// Ask sends a question to the LLM with relevant wiki pages as context and
// returns the synthesized answer with source citations.
func (e *Engine) Ask(ctx context.Context, question string, opts *AskOptions) (*AskResult, error) {
	topN := DefaultTopN
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
	if e.llmProvider == nil {
		return nil, fmt.Errorf("no LLM provider configured — check your config")
	}
	resp, err := e.llmProvider.Chat(ctx, messages, &llm.ChatOptions{
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
	topN := DefaultTopN
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
	if e.llmProvider == nil {
		return nil, fmt.Errorf("no LLM provider configured — check your config")
	}
	llmCh, err := e.llmProvider.ChatStream(ctx, messages, &llm.ChatOptions{
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

		// Signal done with sources so the CLI can print them
		askCh <- AskChunk{Done: true, Sources: sources}
	}()

	return askCh, nil
}

// retrieveContext searches the wiki for pages relevant to the question.
//
// Delegates to Manager.Search, which uses hybrid retrieval (FTS5 + vector
// similarity, RRF-fused) when an embedder is configured, falling back to
// FTS5-only with AND→OR fallback otherwise.
func (e *Engine) retrieveContext(ctx context.Context, question string, topN int) ([]Source, error) {
	results, err := e.wiki.Search(ctx, question, topN)
	if err != nil {
		return nil, err
	}

	sources := make([]Source, 0, len(results))
	for _, r := range results {
		// Validate the page is readable (skip if missing from disk)
		if _, err := e.wiki.ReadByPath(r.Path); err != nil {
			continue
		}

		sources = append(sources, Source{
			Title:   r.Title,
			Path:    r.Path,
			Snippet: stripTags(r.Snippet),
		})
	}

	return sources, nil
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

		// Use "Source N:" heading (not "[N]") to avoid the LLM confusing the
		// index number with citation syntax and outputting [[3]] instead of
		// the actual page title like [[Golang]].
		fmt.Fprintf(&contextBuilder, "### Source %d: %s\n\n", i+1, src.Title)
		// Truncate very long pages to avoid exceeding context window
		content := page.Content
		if len(content) > 4000 {
			content = content[:4000] + "\n\n... (truncated)"
		}
		contextBuilder.WriteString(content)
		contextBuilder.WriteString("\n\n---\n\n")
	}

	// Build a reference list so the LLM can map titles to source numbers
	// if it chooses to use numeric citations.
	var refList strings.Builder
	for i, src := range sources {
		fmt.Fprintf(&refList, "  - Source %d: [[%s]]\n", i+1, src.Title)
	}

	systemPrompt := fmt.Sprintf(`You are a knowledgeable research assistant answering questions based on the user's personal wiki.

Below are relevant wiki pages that may contain the answer. Use ONLY the provided context to answer the question. If the context doesn't contain enough information, say so honestly — do not fabricate facts.

## Citation Rules

When citing a source, use the page TITLE in [[double brackets]]. For example:
  - "According to [[Golang]], Go's GC uses mark-sweep."
  - "[[垃圾回收]] is an automatic memory management technique."

IMPORTANT: Do NOT use numeric references like [[1]] or [[3]] — these are ambiguous.
Use the actual page title every time.

## Reference List (for your reference — cite by title, not number)

%s
%s`, refList.String(), contextBuilder.String())

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
