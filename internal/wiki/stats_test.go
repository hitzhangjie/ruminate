package wiki

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_Stats(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, nil, nil)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	// Seed wiki pages + one raw source.
	pages := []struct {
		title string
		pt    PageType
		body  string
	}{
		{"Src A", PageTypeSummary, "# Src A\nSee [[Closure]] and [[Go]].\n"},
		{"Go", PageTypeEntity, "# Go\nLanguage.\n"},
		{"Closure", PageTypeConcept, "# Closure\nCaptures [[Go]] env.\n"},
		{"Q&A: test", PageTypeSynthesis, "# Q\nA\n"},
	}
	for _, p := range pages {
		if _, err := mgr.Create(p.title, p.pt, p.body); err != nil {
			t.Fatalf("Create %s: %v", p.title, err)
		}
	}
	rawDir := filepath.Join(dir, "raw", "articles")
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "src-a.md"), []byte("# raw"), 0644); err != nil {
		t.Fatal(err)
	}

	st, err := mgr.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Summaries != 1 || st.Entities != 1 || st.Concepts != 1 || st.Synthesis != 1 {
		t.Fatalf("type counts: %+v", st)
	}
	if st.Pages != 4 {
		t.Fatalf("pages = %d", st.Pages)
	}
	if st.Sources != 1 {
		t.Fatalf("sources = %d", st.Sources)
	}
	// Unique link targets: Closure, Go
	if st.Links < 2 {
		t.Fatalf("links = %d, want >= 2", st.Links)
	}
	if len(st.Topics) == 0 {
		t.Fatal("expected topics sample")
	}
}
