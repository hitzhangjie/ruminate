package wiki

import (
	"testing"
)

// testVec creates a simple 4-dimensional vector for testing.
func testVec(vals ...float32) []float32 {
	v := make([]float32, 4)
	for i, val := range vals {
		if i < 4 {
			v[i] = val
		}
	}
	return v
}

func TestMMRDiversify_Normal(t *testing.T) {
	// Simulate two semantic clusters:
	//   Cluster A (GC): vectors near [1, 0, 0, 0]
	//   Cluster B (THP): vectors near [0, 1, 0, 0]
	//   Query: [0.7, 0.3, 0, 0] — relevant to both, slightly more to A

	queryVec := testVec(0.7, 0.3, 0, 0)

	candidates := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "gc1.md", Title: "GC Basics"}}, vector: testVec(0.95, 0.05, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "gc2.md", Title: "GC Advanced"}}, vector: testVec(0.90, 0.10, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "gc3.md", Title: "GC Tuning"}}, vector: testVec(0.85, 0.15, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "thp1.md", Title: "THP Basics"}}, vector: testVec(0.10, 0.90, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "thp2.md", Title: "THP in Linux"}}, vector: testVec(0.05, 0.95, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "mix1.md", Title: "GC and Memory"}}, vector: testVec(0.60, 0.40, 0, 0)},
	}

	// With lambda=0.7, MMR should pick from both clusters.
	// First pick: the one with highest cosine sim to query (gc1 or gc2 or gc3)
	// Later picks: MMR will favor THP content because GC is already covered.

	got := mmrDiversify(queryVec, candidates, 0.7, 5)

	if len(got) != 5 {
		t.Fatalf("expected 5 results, got %d", len(got))
	}

	// Check that we have content from both clusters.
	hasGC := false
	hasTHP := false
	for _, r := range got {
		switch r.Path {
		case "gc1.md", "gc2.md", "gc3.md":
			hasGC = true
		case "thp1.md", "thp2.md":
			hasTHP = true
		}
	}
	if !hasGC {
		t.Error("expected at least one GC-related result")
	}
	if !hasTHP {
		t.Error("expected at least one THP-related result (diversity)")
	}
}

func TestMMRDiversify_LambdaOne(t *testing.T) {
	// λ=1 means pure relevance, no diversity — should behave like top-K by cosine sim.
	// Use vectors where direction matches are clear and unambiguous.

	queryVec := testVec(1, 0, 0, 0)

	candidates := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "best.md", Title: "Best match"}}, vector: testVec(0.99, 0.01, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "worst.md", Title: "Worst match"}}, vector: testVec(0, 1, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "second.md", Title: "Second best"}}, vector: testVec(0.95, 0.05, 0, 0)},
	}

	got := mmrDiversify(queryVec, candidates, 1.0, 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}

	// With λ=1 and query=[1,0,0,0], best.md is closest to the query direction.
	if got[0].Path != "best.md" {
		t.Errorf("expected best.md first (highest relevance), got %s", got[0].Path)
	}
	// second.md should be next (0.95 in x-direction vs 0 for worst.md).
	if got[1].Path != "second.md" {
		t.Errorf("expected second.md second, got %s", got[1].Path)
	}
}

func TestMMRDiversify_LambdaZero(t *testing.T) {
	// λ=0 means pure diversity — should maximize spread.

	queryVec := testVec(1, 0, 0, 0)

	candidates := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}}, vector: testVec(1, 0, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "b.md", Title: "B"}}, vector: testVec(0.99, 0.01, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "c.md", Title: "C"}}, vector: testVec(0, 1, 0, 0)},
	}

	got := mmrDiversify(queryVec, candidates, 0.0, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}

	// First: a.md (highest relevance, always picked first regardless of λ).
	// Second: c.md (maximally different from a.md since λ=0 ignores relevance).
	if got[1].Path != "c.md" {
		t.Errorf("expected c.md second (most diverse from a), got %s", got[1].Path)
	}
}

func TestMMRDiversify_NotEnoughCandidates(t *testing.T) {
	queryVec := testVec(1, 0, 0, 0)
	candidates := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "a.md", Title: "A"}}, vector: testVec(1, 0, 0, 0)},
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "b.md", Title: "B"}}, vector: testVec(0, 1, 0, 0)},
	}

	// Ask for more than available.
	got := mmrDiversify(queryVec, candidates, 0.7, 10)
	if len(got) != 2 {
		t.Fatalf("expected all 2 results when k > len(candidates), got %d", len(got))
	}
}

func TestMMRDiversify_EmptyCandidates(t *testing.T) {
	queryVec := testVec(1, 0, 0, 0)
	got := mmrDiversify(queryVec, nil, 0.7, 5)
	if got != nil {
		t.Fatalf("expected nil for empty candidates, got %v", got)
	}
}

func TestMMRDiversify_SingleCandidate(t *testing.T) {
	queryVec := testVec(1, 0, 0, 0)
	candidates := []scoredResult{
		{SearchResult: SearchResult{IndexEntry: IndexEntry{Path: "only.md", Title: "Only"}}, vector: testVec(1, 0, 0, 0)},
	}

	got := mmrDiversify(queryVec, candidates, 0.7, 5)
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Path != "only.md" {
		t.Errorf("expected only.md, got %s", got[0].Path)
	}
}
