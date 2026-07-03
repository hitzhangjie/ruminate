package query

import (
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// FindOptions controls full-text search behavior.
type FindOptions struct {
	// Limit is the maximum number of results to return.
	// Default: 20.
	Limit int
}

// FindResult is a single full-text search result.
type FindResult struct {
	// Title is the page title.
	Title string
	// Path is the relative page path from wiki root.
	Path string
	// Type is the page category.
	Type wiki.PageType
	// Snippet is a highlighted text fragment showing the match context.
	Snippet string
	// Rank is the BM25 relevance score. Lower means more relevant.
	Rank float64
}

// Find performs a full-text search across all wiki pages and raw sources.
//
// It queries the SQLite FTS5 index (pages_fts) directly — NOT index.md.
// index.md is a human-readable directory derived from pages_fts.
// Results are ranked by BM25 relevance and include highlighted snippets
// with <b>...</b> tags marking the matching terms.
func (e *Engine) Find(query string, opts *FindOptions) ([]FindResult, error) {
	limit := 20
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	results, err := e.wiki.Index().SearchWithSnippets(query, limit)
	if err != nil {
		return nil, err
	}

	findResults := make([]FindResult, len(results))
	for i, r := range results {
		findResults[i] = FindResult{
			Title:   r.Title,
			Path:    r.Path,
			Type:    r.Type,
			Snippet: r.Snippet,
			Rank:    r.Rank,
		}
	}

	return findResults, nil
}
