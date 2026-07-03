package query

import (
	"testing"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

func TestRRFFuse_Normal(t *testing.T) {
	fts := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "a.md", Title: "A"}},
		{IndexEntry: wiki.IndexEntry{Path: "b.md", Title: "B"}},
		{IndexEntry: wiki.IndexEntry{Path: "c.md", Title: "C"}},
	}
	vec := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "b.md", Title: "B"}}, // appears in both lists
		{IndexEntry: wiki.IndexEntry{Path: "d.md", Title: "D"}},
		{IndexEntry: wiki.IndexEntry{Path: "a.md", Title: "A"}}, // appears in both lists
	}

	// FTS ranks: A=1, B=2, C=3
	// Vec ranks: B=1, D=2, A=3
	// RRF scores:
	// A: 1/(60+1) + 1/(60+3) = 0.01639 + 0.01587 = 0.03226
	// B: 1/(60+2) + 1/(60+1) = 0.01613 + 0.01639 = 0.03252  ← highest
	// C: 1/(60+3) + 0          = 0.01587
	// D: 0          + 1/(60+2) = 0.01613

	got := rrfFuse(fts, vec, 4)
	if len(got) != 4 {
		t.Fatalf("expected 4 results, got %d", len(got))
	}
	// B should be first (highest RRF score)
	if got[0].Path != "b.md" {
		t.Errorf("expected B first (RRF 0.03252), got %s", got[0].Path)
	}
	// C should be last (lowest RRF score)
	if got[3].Path != "c.md" && got[3].Path != "d.md" {
		t.Errorf("expected C or D last, got %s", got[3].Path)
	}
}

func TestRRFFuse_Limit(t *testing.T) {
	fts := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "a.md", Title: "A"}},
		{IndexEntry: wiki.IndexEntry{Path: "b.md", Title: "B"}},
	}
	vec := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "c.md", Title: "C"}},
	}

	got := rrfFuse(fts, vec, 2)
	if len(got) != 2 {
		t.Fatalf("expected limit 2, got %d", len(got))
	}
}

func TestRRFFuse_EmptyFTS(t *testing.T) {
	vec := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "a.md", Title: "A"}},
		{IndexEntry: wiki.IndexEntry{Path: "b.md", Title: "B"}},
	}

	got := rrfFuse(nil, vec, 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 results from vector-only, got %d", len(got))
	}
}

func TestRRFFuse_EmptyVector(t *testing.T) {
	fts := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "a.md", Title: "A"}},
	}

	got := rrfFuse(fts, nil, 5)
	if len(got) != 1 {
		t.Fatalf("expected 1 result from FTS-only, got %d", len(got))
	}
}

func TestRRFFuse_EmptyBoth(t *testing.T) {
	got := rrfFuse(nil, nil, 5)
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

func TestRRFFuse_Deduplication(t *testing.T) {
	// Same path appears in both lists — should appear once in output
	fts := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "a.md", Title: "A"}},
	}
	vec := []wiki.SearchResult{
		{IndexEntry: wiki.IndexEntry{Path: "a.md", Title: "A"}},
	}

	got := rrfFuse(fts, vec, 3)
	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(got))
	}
	if got[0].Path != "a.md" {
		t.Errorf("expected a.md, got %s", got[0].Path)
	}
}
