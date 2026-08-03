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
	TopN     int               // Number of diverse search results to use as LLM context.
	Save     bool              // Save the Q&A result as a wiki synthesis page.
	NoStream bool              // Disable streaming output.
	Effort   wiki.SearchEffort // Query expansion effort level (fast/balanced/thorough).
	// Evidence controls L1→L2 layered retrieval (wiki|auto|raw). See docs/108.
	Evidence EvidenceMode
}

// AskResult is the final result of an AI Q&A request.
type AskResult struct {
	Answer  string
	Sources []Source
}

// Source represents a wiki page or raw evidence used as context for an answer.
type Source struct {
	Title   string
	Path    string
	Snippet string
	// Layer is "wiki" (Synthesis) or "raw" (Evidence). Empty means wiki.
	Layer string
	// Content is optional preloaded full text (used for raw evidence to avoid re-read).
	Content string
}

// AskChunk is a streaming fragment of an answer in progress.
type AskChunk struct {
	Content string
	Done    bool
	Error   error
	Sources []Source
}

const DefaultTopN = 20

// Ask sends a question to the LLM with relevant wiki pages as context and
// returns the synthesized answer with source citations.
func (e *Engine) Ask(ctx context.Context, question string, opts *AskOptions) (*AskResult, error) {
	topN := DefaultTopN
	save := false
	effort := wiki.SearchEffortFast
	evidence := EvidenceAuto
	if opts != nil {
		if opts.TopN > 0 {
			topN = opts.TopN
		}
		save = opts.Save
		if opts.Effort != "" {
			effort = opts.Effort
		}
		if opts.Evidence != "" {
			evidence = opts.Evidence
		}
	}

	if e.tracer != nil {
		e.tracer.Begin("ask", "provider", e.llmCfg.Provider, "model", e.llmCfg.Model,
			"query", question, "topN", topN, "effort", string(effort), "evidence", string(evidence))
		defer e.tracer.End("saved", save)
	}

	// 1. Search for relevant pages (L1) + optional Evidence escalation (L2)
	sources, err := e.retrieveContext(ctx, question, topN, effort, evidence)
	if err != nil {
		if e.tracer != nil {
			e.tracer.Error(err)
		}
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

	if e.tracer != nil {
		contextChars := 0
		for _, msg := range messages {
			contextChars += len(msg.Content)
		}
		tokenEst := contextChars / 3
		e.tracer.Begin("context")
		e.tracer.End("sources", len(sources), "chars", contextChars, "tokens_est", tokenEst,
			"docs", sourceDocList(sources))
	}

	// 3. Call LLM
	if e.llmProvider == nil {
		return nil, fmt.Errorf("no LLM provider configured — check your config")
	}
	if e.tracer != nil {
		e.tracer.Begin("llm", "model", e.llmCfg.Model, "temperature",
			fmt.Sprintf("%.2f", e.llmCfg.Temperature), "streaming", false)
	}
	resp, err := e.llmProvider.Chat(ctx, messages, &llm.ChatOptions{
		Temperature: e.llmCfg.Temperature,
	})
	if err != nil {
		if e.tracer != nil {
			e.tracer.Error(err)
		}
		return nil, fmt.Errorf("LLM chat: %w", err)
	}
	if e.tracer != nil {
		e.tracer.End("answer_chars", len(resp.Content))
	}

	result := &AskResult{
		Answer:  resp.Content,
		Sources: sources,
	}

	// 4. Optionally save to wiki
	if save {
		if err := e.saveToWiki(question, result); err != nil {
			if e.tracer != nil {
				e.tracer.Error(err)
			}
			return result, fmt.Errorf("saving to wiki: %w", err)
		}
	}

	return result, nil
}

// AskStream is like Ask but streams the answer as it is generated.
func (e *Engine) AskStream(ctx context.Context, question string, opts *AskOptions) (<-chan AskChunk, error) {
	topN := DefaultTopN
	save := false
	effort := wiki.SearchEffortFast
	evidence := EvidenceAuto
	if opts != nil {
		if opts.TopN > 0 {
			topN = opts.TopN
		}
		save = opts.Save
		if opts.Effort != "" {
			effort = opts.Effort
		}
		if opts.Evidence != "" {
			evidence = opts.Evidence
		}
	}

	if e.tracer != nil {
		e.tracer.Begin("ask", "provider", e.llmCfg.Provider, "model", e.llmCfg.Model,
			"query", question, "topN", topN, "effort", string(effort), "evidence", string(evidence))
		defer e.tracer.End("saved", save)
	}

	// 1. Search for relevant pages (L1) + optional Evidence escalation (L2)
	sources, err := e.retrieveContext(ctx, question, topN, effort, evidence)
	if err != nil {
		if e.tracer != nil {
			e.tracer.Error(err)
		}
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

	if e.tracer != nil {
		contextChars := 0
		for _, msg := range messages {
			contextChars += len(msg.Content)
		}
		tokenEst := contextChars / 3
		e.tracer.Begin("context")
		e.tracer.End("sources", len(sources), "chars", contextChars, "tokens_est", tokenEst,
			"docs", sourceDocList(sources))
	}

	// 3. Start streaming LLM call
	if e.llmProvider == nil {
		return nil, fmt.Errorf("no LLM provider configured — check your config")
	}
	if e.tracer != nil {
		e.tracer.Begin("llm", "model", e.llmCfg.Model, "temperature",
			fmt.Sprintf("%.2f", e.llmCfg.Temperature), "streaming", true)
	}
	llmCh, err := e.llmProvider.ChatStream(ctx, messages, &llm.ChatOptions{
		Temperature: e.llmCfg.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM chat stream: %w", err)
	}

	// 4. Adapt the LLM chunk stream to AskChunk stream
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

		if e.tracer != nil {
			e.tracer.End("answer_chars", fullAnswer.Len())
		}

		askCh <- AskChunk{Done: true, Sources: sources}
	}()

	return askCh, nil
}

// retrieveContext searches the wiki for pages relevant to the question,
// auto-escalating to raw Evidence when needed (docs/108).
func (e *Engine) retrieveContext(ctx context.Context, question string, topN int, effort wiki.SearchEffort, evidence EvidenceMode) ([]Source, error) {
	results, err := e.wiki.Search(ctx, question, topN, effort)
	if err != nil {
		return nil, err
	}

	sources := make([]Source, 0, len(results))
	for _, r := range results {
		if _, err := e.wiki.ReadByPath(r.Path); err != nil {
			continue
		}
		sources = append(sources, Source{
			Title:   r.Title,
			Path:    r.Path,
			Snippet: stripTags(r.Snippet),
			Layer:   "wiki",
		})
	}

	switch evidence {
	case EvidenceRaw:
		sources = e.escalateEvidence(sources, question)
	case EvidenceAuto:
		if needsEvidenceEscalation(question, sources) {
			if e.tracer != nil {
				e.tracer.Begin("evidence_escalation", "reason", "auto")
			}
			sources = e.escalateEvidence(sources, question)
			if e.tracer != nil {
				e.tracer.End("sources", len(sources))
			}
		}
	}

	return sources, nil
}

// escalateEvidence attaches L2 raw content for hit wiki pages.
func (e *Engine) escalateEvidence(wikiSources []Source, question string) []Source {
	// Prefer concrete *wiki.Manager methods when available.
	type fullReader interface {
		ReadByPath(path string) (*wiki.Page, error)
		SearchRaw(query string, topN int) ([]wiki.SearchResult, error)
	}
	if r, ok := e.wiki.(fullReader); ok {
		return attachEvidence(r, wikiSources, question, 48*1024)
	}
	// Fallback: only ReadByPath via wikiManager interface
	return attachEvidence(&readOnlyAdapter{e.wiki}, wikiSources, question, 48*1024)
}

// readOnlyAdapter adapts wikiManager to rawLayerReader without SearchRaw.
type readOnlyAdapter struct {
	wm wikiManager
}

func (a *readOnlyAdapter) ReadByPath(path string) (*wiki.Page, error) {
	return a.wm.ReadByPath(path)
}

func (a *readOnlyAdapter) SearchRaw(query string, topN int) ([]wiki.SearchResult, error) {
	return nil, nil
}

// buildAskMessages constructs the LLM prompt with context and question.
func (e *Engine) buildAskMessages(question string, sources []Source) []llm.Message {
	var contextBuilder strings.Builder
	contextBuilder.WriteString("## Relevant Context\n\n")
	for i, src := range sources {
		layer := src.Layer
		if layer == "" {
			layer = "wiki"
		}
		label := "Wiki (Synthesis)"
		if layer == "raw" {
			label = "Raw (Evidence)"
		}
		fmt.Fprintf(&contextBuilder, "### Source %d: %s [%s]\n\n", i+1, src.Title, label)

		if src.Content != "" {
			contextBuilder.WriteString(src.Content)
		} else {
			page, err := e.wiki.ReadByPath(src.Path)
			if err != nil {
				continue
			}
			contextBuilder.WriteString(page.Content)
		}
		contextBuilder.WriteString("\n\n---\n\n")
	}

	var refList strings.Builder
	for i, src := range sources {
		layer := src.Layer
		if layer == "" {
			layer = "wiki"
		}
		fmt.Fprintf(&refList, "  - Source %d: [[%s]] (%s · %s)\n", i+1, src.Title, layer, src.Path)
	}

	systemPrompt := fmt.Sprintf(`You are a knowledgeable research assistant answering questions based on the user's personal knowledge base.

The knowledge base has two layers (dual truth):
- **Wiki (Synthesis)**: compiled understanding — dense but may omit details (lossy distillation).
- **Raw (Evidence)**: original archived sources — use for precise quotes, defaults, signatures.

Use ONLY the provided context. If the context doesn't contain enough information, say so honestly — do not fabricate facts.
When Evidence and Synthesis disagree, prefer Evidence for factual details and note the discrepancy.
Label whether key claims come from Synthesis vs Evidence when it matters (API behavior, defaults, dates, numbers).

## Citation Rules

When citing a source, use the page TITLE in [[double brackets]]. For example:
  - "According to [[Golang]], Go's GC uses mark-sweep."
  - "[[垃圾回收]] is an automatic memory management technique."

IMPORTANT: Do NOT use numeric references like [[1]] or [[3]] — these are ambiguous.
Use the actual page title every time. For raw evidence you may also cite the path.

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

	existing, err := e.wiki.Read(title, wiki.PageTypeSynthesis)
	if err != nil {
		_, err = e.wiki.Create(title, wiki.PageTypeSynthesis, b.String())
		return err
	}
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

// sourceDocList formats source titles for trace output (compact, readable).
func sourceDocList(sources []Source) string {
	if len(sources) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, src := range sources {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(src.Title)
		if i >= 9 {
			fmt.Fprintf(&b, ",…(%d total)", len(sources))
			break
		}
	}
	b.WriteString("]")
	return b.String()
}
