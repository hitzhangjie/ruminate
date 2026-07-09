package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// AnalysisResult is the structured output from LLM analysis of a source.
//
// After a source (file/URL) is ingested, the LLM decomposes it into the
// following components. Each component maps to a specific part of the wiki:
//
//   - Summary → summary page body (section "## Summary")
//   - KeyPoints → summary page body (section "## Key Points"), as bullet list
//   - Entities → standalone entity pages under [[entities:...]], linked from
//     the summary page. An entity is a concrete, identifiable thing: a person,
//     an event, a technical term, an organization, etc.
//   - Concepts → standalone concept pages under [[concepts:...]], linked from
//     the summary page. A concept is an abstract idea, theme, or framework
//     that runs through the source.
//   - Tags → inline tags rendered on the summary page (section "## Tags"),
//     usable for future filtering/search.
//
// Entities and Concepts are deduplicated by name: if a page already exists
// for that name, new content from this source is appended as a new section
// rather than overwriting the existing page.
type AnalysisResult struct {
	// Summary is a 2-5 sentence distillation of the source's main ideas.
	// Written as the body of the summary page.
	Summary string `json:"summary"`

	// Entities are the concrete, identifiable things extracted from the source
	// (people, events, technical terms, organizations, etc.).
	// Each becomes its own entity page; the summary page links to each.
	Entities []EntityInfo `json:"entities"`

	// Concepts are the abstract ideas, themes, or frameworks extracted from
	// the source. Each becomes its own concept page; the summary page links to each.
	Concepts []ConceptInfo `json:"concepts"`

	// KeyPoints are the 3-7 most important takeaways from the source.
	// Rendered as a bullet list in the summary page.
	KeyPoints []string `json:"key_points"`

	// Tags are 3-7 descriptive labels for filtering and search.
	// Rendered as inline code spans on the summary page.
	Tags []string `json:"tags"`
}

// EntityInfo represents a concrete, identifiable thing extracted from a source.
//
// Entities are long-lived wiki objects that accumulate references across
// multiple ingest runs. When the same entity name appears in a new source,
// the new description is appended to the existing entity page rather than
// replacing it (see mergeEntityContent).
type EntityInfo struct {
	// Name is the canonical name of this entity (e.g. "Alan Turing",
	// "World War II", "Kubernetes"). Used as the wiki page title under
	// the "entities:" namespace.
	Name string `json:"name"`

	// Type classifies the entity for filtering and display purposes.
	// Valid values: "person", "event", "term", "organization", "other".
	Type string `json:"type"`

	// Description is a 1-2 sentence explanation of who or what this entity is,
	// as extracted from the current source. When multiple sources reference the
	// same entity, each source's description is preserved in its own section.
	Description string `json:"description"`
}

// ConceptInfo represents an abstract idea, theme, or framework extracted
// from a source.
//
// Concepts differ from entities: an entity is a concrete noun (person, event,
// organization), while a concept is an abstract notion (e.g. "Technical Debt",
// "Attention Mechanism", "Opportunity Cost"). Like entities, concepts are
// long-lived and accumulate references across multiple ingest runs.
type ConceptInfo struct {
	// Name is the canonical name of this concept (e.g. "Technical Debt",
	// "Attention Mechanism"). Used as the wiki page title under the
	// "concepts:" namespace.
	Name string `json:"name"`

	// Description is a 1-2 sentence explanation of this concept as extracted
	// from the current source. When multiple sources discuss the same concept,
	// each source's description is preserved in its own section.
	Description string `json:"description"`
}

// Engine drives the ingest pipeline: read → analyze → update wiki → commit.
//
// Engine holds its own LLM provider reference rather than going through
// wiki.Manager — Manager's LLM provider serves write-path needs (e.g. embeddings),
// while Engine's provider serves the analysis phase (inference for source
// decomposition into summary/entities/concepts).
type Engine struct {
	wiki        *wiki.Manager
	reader      *Reader
	llmProvider llm.LLMProvider
	llmCfg      config.LLMConfig
}

// NewEngine creates a new ingest Engine from the given runtime configuration.
// It internally initializes the wiki.Manager and validates that the wiki
// is initialized. Returns an error if the wiki has not been initialized yet.
// The LLM provider is initialized from cfg; if unavailable, it stays nil
// (callers should check before calling Ingest).
func NewEngine(cfg *config.RuntimeConfig) (*Engine, error) {
	mgr, err := wiki.NewManagerFromConfig(cfg.WikiPath, cfg.LLM, cfg.Embedding)
	if err != nil {
		return nil, err
	}
	if !mgr.IsInitialized() {
		return nil, fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
	}

	// Initialize LLM provider for this engine's analysis needs.
	// Non-fatal: provider stays nil if unreachable.
	var llmProvider llm.LLMProvider
	if cfg.LLM.Provider != "" {
		provider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model)
		if err == nil {
			llmProvider = provider
		}
	}

	return &Engine{
		wiki:        mgr,
		reader:      NewReader(),
		llmProvider: llmProvider,
		llmCfg:      cfg.LLM,
	}, nil
}

// IsSupportedExtension reports whether the Reader has a registered handler
// for the given file extension. The extension should include the leading dot
// (e.g. ".md"). Matching is case-insensitive.
//
// This is used by the sync engine to filter files before attempting ingestion.
func (e *Engine) IsSupportedExtension(ext string) bool {
	return e.reader.IsSupportedExtension(ext)
}

// Ingest processes a source (file path or URL) end-to-end:
//  1. Read source
//  2. Save raw copy
//  3. LLM analysis
//  4. Create/update wiki pages
//  5. Git commit
//
// sourceType is a user-defined label: "article", "paper", "note", "book".
func (e *Engine) Ingest(ctx context.Context, sourcePath, sourceType string) error {
	// 1. Read source
	src, err := e.reader.Read(sourcePath, sourceType)
	if err != nil {
		return fmt.Errorf("reading source: %w", err)
	}

	// 2. Save raw copy (also indexes in FTS5 for full-text search)
	if _, err := e.wiki.AddSource(src.SourceType, src.Title, []byte(src.Content)); err != nil {
		return fmt.Errorf("saving raw source: %w", err)
	}

	// 3. LLM analysis
	analysis, err := e.analyze(ctx, src)
	if err != nil {
		return fmt.Errorf("analyzing source: %w", err)
	}

	// 4. Create/update summary page
	if err := e.createSummaryPage(src, analysis); err != nil {
		return fmt.Errorf("creating summary: %w", err)
	}

	// 5. Create/update entity pages
	if err := e.createEntityPages(src, analysis); err != nil {
		return fmt.Errorf("creating entities: %w", err)
	}

	// 6. Create/update concept pages
	if err := e.createConceptPages(src, analysis); err != nil {
		return fmt.Errorf("creating concepts: %w", err)
	}

	// 7. Git commit
	if err := e.wiki.Git().AddAll(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	commitMsg := fmt.Sprintf("[ingest] %s", src.Title)
	if err := e.wiki.Git().Commit(commitMsg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

// analyze sends the source content to the LLM and parses the structured response.
func (e *Engine) analyze(ctx context.Context, src *Source) (*AnalysisResult, error) {
	// Validate content size against model's max input tokens.
	// The system prompt also counts toward the input, so we deduct its estimated tokens.
	sysPromptTokens := estimateTokens(ingestSystemPrompt)
	effectiveMaxTokens := e.llmCfg.MaxInputTokens - sysPromptTokens
	if err := validateContentSize(src.Content, effectiveMaxTokens); err != nil {
		return nil, err
	}

	if e.llmProvider == nil {
		return nil, fmt.Errorf("no LLM provider configured — check your config")
	}

	messages := []llm.Message{
		{Role: "system", Content: ingestSystemPrompt},
		{Role: "user", Content: src.Content},
	}

	resp, err := e.llmProvider.Chat(ctx, messages, &llm.ChatOptions{
		Temperature: e.llmCfg.Temperature,
	})
	if err != nil {
		return nil, err
	}

	return parseAnalysisResponse(resp.Content)
}

// createSummaryPage creates or updates the summary wiki page for the source.
func (e *Engine) createSummaryPage(src *Source, analysis *AnalysisResult) error {
	rawPath := e.wiki.RawSourcePath(src.SourceType, src.Title)
	content := buildSummaryContent(src, analysis, rawPath)

	// Check if a summary for this source already exists
	existing, err := e.wiki.Read(src.Title, wiki.PageTypeSummary)
	if err != nil {
		// Page doesn't exist — create it
		_, err = e.wiki.Create(src.Title, wiki.PageTypeSummary, content)
		return err
	}

	// Page exists — update it
	_, err = e.wiki.Update(existing.Title, wiki.PageTypeSummary, content)
	return err
}

// createEntityPages creates or updates entity pages extracted from the source.
func (e *Engine) createEntityPages(src *Source, analysis *AnalysisResult) error {
	for _, entity := range analysis.Entities {
		content := buildEntityContent(entity, src)

		existing, err := e.wiki.Read(entity.Name, wiki.PageTypeEntity)
		if err != nil {
			// Create new entity page
			if _, err := e.wiki.Create(entity.Name, wiki.PageTypeEntity, content); err != nil {
				return fmt.Errorf("creating entity %q: %w", entity.Name, err)
			}
			continue
		}

		// Update existing entity — append new reference section
		newContent := mergeEntityContent(existing.Content, entity, src)
		if _, err := e.wiki.Update(existing.Title, wiki.PageTypeEntity, newContent); err != nil {
			return fmt.Errorf("updating entity %q: %w", entity.Name, err)
		}
	}
	return nil
}

// createConceptPages creates or updates concept pages extracted from the source.
func (e *Engine) createConceptPages(src *Source, analysis *AnalysisResult) error {
	for _, concept := range analysis.Concepts {
		content := buildConceptContent(concept, src)

		existing, err := e.wiki.Read(concept.Name, wiki.PageTypeConcept)
		if err != nil {
			if _, err := e.wiki.Create(concept.Name, wiki.PageTypeConcept, content); err != nil {
				return fmt.Errorf("creating concept %q: %w", concept.Name, err)
			}
			continue
		}

		newContent := mergeConceptContent(existing.Content, concept, src)
		if _, err := e.wiki.Update(existing.Title, wiki.PageTypeConcept, newContent); err != nil {
			return fmt.Errorf("updating concept %q: %w", concept.Name, err)
		}
	}
	return nil
}

// buildSummaryContent generates the markdown content for a summary page.
//
// rawPath is the relative path to the archived raw source file
// (e.g. "raw/article/My Article.md"), used to generate a backlink so readers
// can trace structured knowledge back to the original text.
func buildSummaryContent(src *Source, analysis *AnalysisResult, rawPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", src.Title)

	// Source provenance line — links back to the original URL (if applicable)
	// and to the raw archived copy in the wiki.
	b.WriteString("> **Source**")
	if isURL(src.Origin) {
		fmt.Fprintf(&b, ": [%s](%s)", src.Origin, src.Origin)
	} else {
		fmt.Fprintf(&b, ": `%s`", src.Origin)
	}
	fmt.Fprintf(&b, " | **Type**: %s", src.SourceType)
	if rawPath != "" {
		fmt.Fprintf(&b, " | [📄 raw](%s)", filepathToLink(rawPath))
	}
	b.WriteString("\n\n")

	b.WriteString("## Summary\n\n")
	b.WriteString(analysis.Summary)
	b.WriteString("\n\n")

	if len(analysis.KeyPoints) > 0 {
		b.WriteString("## Key Points\n\n")
		for _, p := range analysis.KeyPoints {
			fmt.Fprintf(&b, "- %s\n", p)
		}
		b.WriteString("\n")
	}

	if len(analysis.Entities) > 0 {
		b.WriteString("## Entities\n\n")
		for _, ent := range analysis.Entities {
			link := wiki.GenerateWikiLink("entities:" + ent.Name)
			fmt.Fprintf(&b, "- %s — %s\n", link, ent.Description)
		}
		b.WriteString("\n")
	}

	if len(analysis.Concepts) > 0 {
		b.WriteString("## Concepts\n\n")
		for _, con := range analysis.Concepts {
			link := wiki.GenerateWikiLink("concepts:" + con.Name)
			fmt.Fprintf(&b, "- %s — %s\n", link, con.Description)
		}
		b.WriteString("\n")
	}

	if len(analysis.Tags) > 0 {
		b.WriteString("## Tags\n\n")
		for _, tag := range analysis.Tags {
			fmt.Fprintf(&b, "`%s` ", tag)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// buildEntityContent generates the markdown content for a new entity page.
func buildEntityContent(entity EntityInfo, src *Source) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", entity.Name)
	fmt.Fprintf(&b, "**Type**: %s\n\n", entity.Type)
	b.WriteString(entity.Description)
	b.WriteString("\n\n## References\n\n")
	fmt.Fprintf(&b, "- %s (from [[summaries:%s]])\n", entity.Description, src.Title)
	return b.String()
}

// buildConceptContent generates the markdown content for a new concept page.
func buildConceptContent(concept ConceptInfo, src *Source) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", concept.Name)
	b.WriteString(concept.Description)
	b.WriteString("\n\n## References\n\n")
	fmt.Fprintf(&b, "- Discussed in [[summaries:%s]]\n", src.Title)
	return b.String()
}

// mergeEntityContent appends new information from a source to an existing entity page.
func mergeEntityContent(existingContent string, entity EntityInfo, src *Source) string {
	// Avoid duplicate sections: check if this source is already referenced
	if strings.Contains(existingContent, src.Title) {
		// Replace the existing section for this source
		// Simple approach: just return existing content unchanged
		return existingContent
	}

	section := fmt.Sprintf("\n\n## From %s\n\n%s\n", src.Title, entity.Description)
	return existingContent + section
}

// mergeConceptContent appends new information to an existing concept page.
func mergeConceptContent(existingContent string, concept ConceptInfo, src *Source) string {
	if strings.Contains(existingContent, src.Title) {
		return existingContent
	}

	section := fmt.Sprintf("\n\n## From %s\n\n%s\n", src.Title, concept.Description)
	return existingContent + section
}

// parseAnalysisResponse extracts the JSON analysis result from an LLM response.
// It handles models that wrap JSON in markdown code fences.
func parseAnalysisResponse(raw string) (*AnalysisResult, error) {
	jsonStr := raw

	// Try to extract JSON from markdown code fences
	if idx := strings.Index(raw, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(raw[start:], "```"); end != -1 {
			jsonStr = strings.TrimSpace(raw[start : start+end])
		}
	} else if idx := strings.Index(raw, "```"); idx != -1 {
		start := idx + len("```")
		if end := strings.Index(raw[start:], "```"); end != -1 {
			jsonStr = strings.TrimSpace(raw[start : start+end])
		}
	}

	// Also handle models that output text before/after the JSON
	if idx := strings.Index(jsonStr, "{"); idx != -1 {
		if end := strings.LastIndex(jsonStr, "}"); end != -1 && end > idx {
			jsonStr = jsonStr[idx : end+1]
		}
	}

	var result AnalysisResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parsing analysis JSON: %w\nRaw response:\n%s", err, raw)
	}

	return &result, nil
}

// filepathToLink converts a relative filesystem path to a Markdown link target.
// From a summary page at "wiki/summaries/{title}.md", the raw source is at
// "../raw/{sourceType}/{file}.md", so we prepend "../../" to the raw path.
func filepathToLink(rawPath string) string {
	return "../../" + rawPath
}

// estimateTokens provides a rough estimate of token count from a string.
//
// The heuristic is ~4 characters per token for English text. For mixed-language
// content (e.g., Chinese + English), the actual ratio is closer to 2-3 chars/token.
// We use a conservative 4 chars/token to err on the side of allowing more content
// rather than rejecting valid input. This is only a pre-flight check — the actual
// token count depends on the model's tokenizer.
func estimateTokens(content string) int {
	if len(content) == 0 {
		return 0
	}
	return len(content) / 4
}

// validateContentSize checks whether the estimated token count of content exceeds
// maxInputTokens. If it does, it returns a descriptive error so the user knows
// the content is too large and can decide to trim it or increase the limit.
//
// Unlike the old truncateContent, this does NOT silently truncate — silently
// dropping information risks degrading analysis quality in ways the user can't see.
func validateContentSize(content string, maxInputTokens int) error {
	if maxInputTokens <= 0 {
		return nil // no limit configured
	}
	est := estimateTokens(content)
	if est > maxInputTokens {
		return fmt.Errorf(
			"content too large: estimated %d tokens exceeds max input limit of %d tokens (%.1f KB text). "+
				"Either trim the source material or increase max_input_tokens in your config for the current model",
			est, maxInputTokens, float64(len(content))/1024,
		)
	}
	return nil
}

// ingestSystemPrompt instructs the LLM how to analyze source material.
const ingestSystemPrompt = `You are a knowledge extraction assistant. Your task is to analyze the given content and extract structured knowledge for a personal wiki.

Output ONLY a valid JSON object (no markdown, no preamble, no explanation). The JSON must have this exact structure:

{
  "summary": "A concise summary of the content (2-5 sentences, capturing the main ideas)",
  "entities": [
    {
      "name": "Entity Name",
      "type": "person|event|term|organization|other",
      "description": "Brief 1-2 sentence description of this entity"
    }
  ],
  "concepts": [
    {
      "name": "Concept Name",
      "description": "Brief 1-2 sentence explanation of this concept"
    }
  ],
  "key_points": ["Key takeaway 1", "Key takeaway 2", "Key takeaway 3"],
  "tags": ["tag1", "tag2", "tag3"]
}

Rules:
- Extract 3-8 most important entities (people, events, terms, organizations, etc.)
- Extract 2-5 key concepts or themes
- List 3-7 key points
- Provide 3-7 descriptive tags
- Write all content in the same language as the source material
- Be accurate and factual — do not fabricate information
- Keep descriptions concise and informative`
