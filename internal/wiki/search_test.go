package wiki

import (
	"testing"
)

func TestRRFFuse_Normal(t *testing.T) {
	fts := []SearchResult{
		{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}},
		{IndexEntry: IndexEntry{Path: "b.md", Title: "B"}},
		{IndexEntry: IndexEntry{Path: "c.md", Title: "C"}},
	}
	vec := []SearchResult{
		{IndexEntry: IndexEntry{Path: "b.md", Title: "B"}}, // appears in both lists
		{IndexEntry: IndexEntry{Path: "d.md", Title: "D"}},
		{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}}, // appears in both lists
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
	fts := []SearchResult{
		{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}},
		{IndexEntry: IndexEntry{Path: "b.md", Title: "B"}},
	}
	vec := []SearchResult{
		{IndexEntry: IndexEntry{Path: "c.md", Title: "C"}},
	}

	got := rrfFuse(fts, vec, 2)
	if len(got) != 2 {
		t.Fatalf("expected limit 2, got %d", len(got))
	}
}

func TestRRFFuse_EmptyFTS(t *testing.T) {
	vec := []SearchResult{
		{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}},
		{IndexEntry: IndexEntry{Path: "b.md", Title: "B"}},
	}

	got := rrfFuse(nil, vec, 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 results from vector-only, got %d", len(got))
	}
}

func TestRRFFuse_EmptyVector(t *testing.T) {
	fts := []SearchResult{
		{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}},
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

func TestToFTS5OrQuery_MixedScript(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "English and Chinese without space",
			query: "golang垃圾回收",
			want:  `"golang" OR ("垃圾" OR "圾回" OR "回收")`,
		},
		{
			name:  "English and Chinese with space",
			query: "golang 垃圾回收",
			want:  `"golang" OR ("垃圾" OR "圾回" OR "回收")`,
		},
		{
			name:  "Chinese only",
			query: "垃圾回收",
			want:  `("垃圾" OR "圾回" OR "回收")`,
		},
		{
			name:  "English only",
			query: "golang",
			want:  `"golang"`,
		},
		{
			name:  "Chinese then English without space",
			query: "垃圾回收golang",
			want:  `("垃圾" OR "圾回" OR "回收") OR "golang"`,
		},
		{
			name:  "Multiple script transitions",
			query: "go垃圾回收runtime",
			want:  `"go" OR ("垃圾" OR "圾回" OR "回收") OR "runtime"`,
		},
		{
			name:  "English Chinese English with spaces",
			query: "go 垃圾回收 runtime",
			want:  `"go" OR ("垃圾" OR "圾回" OR "回收") OR "runtime"`,
		},
		{
			name:  "Short tokens filtered out",
			query: "a 垃圾回收 b",
			want:  `("垃圾" OR "圾回" OR "回收")`,
		},
		{
			name:  "Punctuation as boundary",
			query: "golang,垃圾回收",
			want:  `"golang" OR ("垃圾" OR "圾回" OR "回收")`,
		},
		{
			name:  "CJK bigram for 2-char term",
			query: "透明巨页 Go GC",
			want:  `("透明" OR "明巨" OR "巨页") OR "Go" OR "GC"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toFTS5OrQuery(tt.query)
			if got != tt.want {
				t.Errorf("toFTS5OrQuery(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestSplitForFTS5Query(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "English and Chinese without space",
			query: "golang垃圾回收",
			want:  []string{"golang", "垃圾回收"},
		},
		{
			name:  "Chinese and English without space",
			query: "垃圾回收golang",
			want:  []string{"垃圾回收", "golang"},
		},
		{
			name:  "Multiple transitions",
			query: "go垃圾回收runtime",
			want:  []string{"go", "垃圾回收", "runtime"},
		},
		{
			name:  "With spaces",
			query: "golang 垃圾回收",
			want:  []string{"golang", "垃圾回收"},
		},
		{
			name:  "Chinese only",
			query: "垃圾回收",
			want:  []string{"垃圾回收"},
		},
		{
			name:  "English only",
			query: "golang",
			want:  []string{"golang"},
		},
		{
			name:  "With punctuation",
			query: "golang,垃圾回收",
			want:  []string{"golang", "垃圾回收"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitForFTS5Query(tt.query)
			if len(got) != len(tt.want) {
				t.Errorf("splitForFTS5Query(%q) = %v (len=%d), want %v (len=%d)",
					tt.query, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitForFTS5Query(%q)[%d] = %q, want %q",
						tt.query, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRRFFuse_Deduplication(t *testing.T) {
	// Same path appears in both lists — should appear once in output
	fts := []SearchResult{
		{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}},
	}
	vec := []SearchResult{
		{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}},
	}

	got := rrfFuse(fts, vec, 3)
	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(got))
	}
	if got[0].Path != "a.md" {
		t.Errorf("expected a.md, got %s", got[0].Path)
	}
}
