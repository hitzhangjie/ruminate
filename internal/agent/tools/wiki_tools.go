package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// WikiStore is the subset of wiki.Manager used by knowledge tools.
type WikiStore interface {
	// SearchKeyword is FTS-only (no embed / expansion / MMR). Agent tools must
	// use this rather than the hybrid ask pipeline.
	SearchKeyword(query string, topN int) ([]wiki.SearchResult, error)
	SearchRaw(query string, topN int) ([]wiki.SearchResult, error)
	ReadByPath(path string) (*wiki.Page, error)
	ListSources(sourceType string) ([]string, error)
	Root() string
	WikiDir() string
	RawDir() string
}

// ---- wiki_index ----

type wikiIndexTool struct {
	store WikiStore
}

func (t *wikiIndexTool) Schema() Schema {
	return Schema{
		Name:        "wiki_index",
		Description: "Read the wiki catalog (index.md): titles, paths, one-line summaries by category. Prefer this first to discover relevant pages, then wiki_read to drill down. Optional filter keeps only matching lines (case-insensitive substring).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filter": map[string]any{
					"type":        "string",
					"description": "Optional case-insensitive substring filter over index lines (e.g. \"fmt\", \"GC\")",
				},
			},
		},
	}
}

func (t *wikiIndexTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	page, err := t.store.ReadByPath("index.md")
	if err != nil {
		return "", fmt.Errorf("read index.md: %w", err)
	}
	content := page.Content
	filter := strings.TrimSpace(argString(args, "filter"))
	if filter != "" {
		content = filterLines(content, filter)
		if strings.TrimSpace(content) == "" {
			return fmt.Sprintf("No index.md lines matched filter %q. Try wiki_search or a broader filter.", filter), nil
		}
	}
	return content, nil
}

// filterLines keeps section headings and lines containing needle (case-insensitive).
func filterLines(content, needle string) string {
	lowerNeedle := strings.ToLower(needle)
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		// Keep markdown headings so category structure survives filtering.
		if strings.HasPrefix(trim, "#") {
			out = append(out, line)
			continue
		}
		if strings.Contains(strings.ToLower(line), lowerNeedle) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// ---- wiki_search ----

type wikiSearchTool struct {
	store WikiStore
}

func (t *wikiSearchTool) Schema() Schema {
	return Schema{
		Name:        "wiki_search",
		Description: "FTS keyword search over wiki pages (BM25 + snippets only — no embedding, no query expansion). Use when you know keywords; prefer wiki_index for browsing the catalog.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Keyword query"},
				"top_n": map[string]any{"type": "integer", "description": "Max results (default 8)"},
			},
			"required": []string{"query"},
		},
	}
}

func (t *wikiSearchTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	q := argString(args, "query")
	if q == "" {
		return "", fmt.Errorf("query is required")
	}
	topN := argInt(args, "top_n", 8)
	results, err := t.store.SearchKeyword(q, topN)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No wiki pages matched. Try wiki_index with a filter, different keywords, or escalate to raw_search.", nil
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s (%s)\n   snippet: %s\n", i+1, r.Title, r.Path, cleanSnippet(r.Snippet))
	}
	return b.String(), nil
}

// ---- wiki_read ----

type wikiReadTool struct {
	store WikiStore
}

func (t *wikiReadTool) Schema() Schema {
	return Schema{
		Name:        "wiki_read",
		Description: "Read a wiki page by relative path (e.g. wiki/concepts/GC.md).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Relative path from wiki root"},
			},
			"required": []string{"path"},
		},
	}
}

func (t *wikiReadTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	page, err := t.store.ReadByPath(path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\npath: %s\n", page.Title, page.Path)
	if len(page.Sources) > 0 {
		b.WriteString("sources:\n")
		for _, s := range page.Sources {
			fmt.Fprintf(&b, "  - %s\n", s.Path)
		}
	}
	b.WriteString("\n")
	b.WriteString(page.Content)
	return b.String(), nil
}

// ---- wiki_links ----

type wikiLinksTool struct {
	store WikiStore
}

func (t *wikiLinksTool) Schema() Schema {
	return Schema{
		Name:        "wiki_links",
		Description: "List WikiLink targets found on a page.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}
}

func (t *wikiLinksTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := argString(args, "path")
	page, err := t.store.ReadByPath(path)
	if err != nil {
		return "", err
	}
	if len(page.Links) == 0 {
		return "No WikiLinks on this page.", nil
	}
	return "Links:\n- " + strings.Join(page.Links, "\n- "), nil
}

// ---- raw_list_sources ----

type rawListSourcesTool struct {
	store WikiStore
}

func (t *rawListSourcesTool) Schema() Schema {
	return Schema{
		Name:        "raw_list_sources",
		Description: "List contributing Evidence sources (frontmatter) for a wiki page, or list all raw files if path empty.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Wiki page path; empty = list all raw files"},
			},
		},
	}
}

func (t *rawListSourcesTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path != "" {
		page, err := t.store.ReadByPath(path)
		if err != nil {
			return "", err
		}
		if len(page.Sources) == 0 {
			return "No sources in frontmatter for " + path, nil
		}
		var b strings.Builder
		for _, s := range page.Sources {
			fmt.Fprintf(&b, "- %s", s.Path)
			if s.IngestedAt != "" {
				fmt.Fprintf(&b, " (ingested %s)", s.IngestedAt)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	}
	list, err := t.store.ListSources("")
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No raw sources archived.", nil
	}
	// Cap listing
	const max = 50
	if len(list) > max {
		return strings.Join(list[:max], "\n") + fmt.Sprintf("\n…(%d more)", len(list)-max), nil
	}
	return strings.Join(list, "\n"), nil
}

// ---- raw_read ----

type rawReadTool struct {
	store WikiStore
}

func (t *rawReadTool) Schema() Schema {
	return Schema{
		Name:        "raw_read",
		Description: "Read a raw Evidence file by relative path (e.g. raw/article/foo.md).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}
}

func (t *rawReadTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := argString(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(filepathToSlash(path), "raw/") {
		// Still allow if under store — but prefer raw/
	}
	page, err := t.store.ReadByPath(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("# %s\npath: %s\n\n%s", page.Title, page.Path, page.Content), nil
}

// ---- raw_search ----

type rawSearchTool struct {
	store WikiStore
}

func (t *rawSearchTool) Schema() Schema {
	return Schema{
		Name:        "raw_search",
		Description: "Search raw Evidence (L2) via independent raw_fts. Does not mix with wiki ranking.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"top_n": map[string]any{"type": "integer"},
			},
			"required": []string{"query"},
		},
	}
}

func (t *rawSearchTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	q := argString(args, "query")
	if q == "" {
		return "", fmt.Errorf("query is required")
	}
	topN := argInt(args, "top_n", 5)
	results, err := t.store.SearchRaw(q, topN)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No raw sources matched.", nil
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s (%s)\n   snippet: %s\n", i+1, r.Title, r.Path, cleanSnippet(r.Snippet))
	}
	return b.String(), nil
}

func cleanSnippet(s string) string {
	s = strings.ReplaceAll(s, "<b>", "")
	s = strings.ReplaceAll(s, "</b>", "")
	return strings.TrimSpace(s)
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// RegisterKnowledgeTools registers wiki_* and raw_* tools.
func RegisterKnowledgeTools(r *Registry, store WikiStore) {
	r.Register(&wikiIndexTool{store: store})
	r.Register(&wikiSearchTool{store: store})
	r.Register(&wikiReadTool{store: store})
	r.Register(&wikiLinksTool{store: store})
	r.Register(&rawListSourcesTool{store: store})
	r.Register(&rawReadTool{store: store})
	r.Register(&rawSearchTool{store: store})
}
