package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexManager_Init(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	dbPath := filepath.Join(dir, "fts.db")

	im := NewIndexManager(indexPath, dbPath)
	if err := im.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer im.Close()

	// Verify index.md was created
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !strings.Contains(string(content), "# Wiki Index") {
		t.Errorf("index.md should contain header")
	}

	// Verify FTS database was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("FTS database file should exist at %s", dbPath)
	}
}

func TestIndexManager_AddAndUpdatePage(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	dbPath := filepath.Join(dir, "fts.db")

	im := NewIndexManager(indexPath, dbPath)
	if err := im.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer im.Close()

	page := &Page{
		Title:   "karpathy",
		Path:    "wiki/entities/karpathy.md",
		Type:    PageTypeEntity,
		Content: "# Andrej Karpathy\n\nAI researcher and educator.",
		Links:   []string{"tesla", "openai"},
	}

	// Add page
	if err := im.AddPage(page); err != nil {
		t.Fatalf("AddPage() error: %v", err)
	}

	// Verify index.md now contains the entry
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !strings.Contains(string(content), "karpathy") {
		t.Errorf("index.md should contain 'karpathy', got:\n%s", string(content))
	}

	// Update page
	page.Content = "# Andrej Karpathy\n\nUpdated content."
	if err := im.UpdatePage(page); err != nil {
		t.Fatalf("UpdatePage() error: %v", err)
	}

	// Verify FTS search works
	results, err := im.Search("karpathy", 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search should find exactly 1 'karpathy', got %d", len(results))
	}
	p := results[0]
	if p.Title != page.Title || p.Path != page.Path || p.Type != page.Type {
		t.Errorf("Search the updated page fields should match original page")
	}
}

func TestIndexManager_Search(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	dbPath := filepath.Join(dir, "fts.db")

	im := NewIndexManager(indexPath, dbPath)
	if err := im.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer im.Close()

	pages := []*Page{
		{
			Title:   "karpathy",
			Path:    "wiki/entities/karpathy.md",
			Type:    PageTypeEntity,
			Content: "Andrej Karpathy is an AI researcher. He worked at OpenAI and Tesla.",
		},
		{
			Title:   "deep-learning",
			Path:    "wiki/concepts/deep-learning.md",
			Type:    PageTypeConcept,
			Content: "Deep learning is a subset of machine learning using neural networks.",
		},
		{
			Title:   "llm-wiki-idea",
			Path:    "wiki/summaries/llm-wiki-idea.md",
			Type:    PageTypeSummary,
			Content: "Karpathy's LLM Wiki idea: use LLMs to maintain a personal knowledge base.",
		},
	}

	for _, p := range pages {
		if err := im.AddPage(p); err != nil {
			t.Fatalf("AddPage(%q) error: %v", p.Title, err)
		}
	}

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantTitle string
	}{
		{
			name:      "exact match",
			query:     "karpathy",
			wantCount: 2, // appears in entities/karpathy and summaries/llm-wiki-idea
			wantTitle: "",
		},
		{
			name:      "concept search",
			query:     "deep learning",
			wantCount: 1,
			wantTitle: "deep-learning",
		},
		{
			name:      "no match",
			query:     "nonexistent",
			wantCount: 0,
			wantTitle: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := im.Search(tt.query, 10)
			if err != nil {
				t.Fatalf("Search() error: %v", err)
			}
			if len(results) != tt.wantCount {
				t.Errorf("Search(%q) returned %d results, want %d", tt.query, len(results), tt.wantCount)
			}
		})
	}
}

func TestIndexManager_RemovePage(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	dbPath := filepath.Join(dir, "fts.db")

	im := NewIndexManager(indexPath, dbPath)
	if err := im.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer im.Close()

	page := &Page{
		Title:   "temp-page",
		Path:    "wiki/entities/temp-page.md",
		Type:    PageTypeEntity,
		Content: "This is a temporary page that will be removed.",
	}

	if err := im.AddPage(page); err != nil {
		t.Fatalf("AddPage() error: %v", err)
	}

	// Verify it's searchable (search for a word in the content)
	results, _ := im.Search("temporary", 10)
	if len(results) == 0 {
		t.Fatal("page should be searchable before removal")
	}

	// Remove it
	if err := im.RemovePage(page.Path); err != nil {
		t.Fatalf("RemovePage() error: %v", err)
	}

	// Verify it's gone
	results, _ = im.Search("temporary", 10)
	if len(results) != 0 {
		t.Errorf("page should not be searchable after removal")
	}
}

func TestParseIndexMd(t *testing.T) {
	content := `# Wiki Index

> This index is automatically maintained.

## Summaries
- [[llm-wiki-idea]](wiki/summaries/llm-wiki-idea.md) — 2026-07-01

## Entities
- [[karpathy]](wiki/entities/karpathy.md) — 2026-07-01
- [[tesla]](wiki/entities/tesla.md) — 2026-06-30

## Concepts
*No concepts yet.*

## Synthesis
- [[design-philosophy]](wiki/synthesis/design-philosophy.md) — 2026-06-28
`

	entries := parseIndexMd(content)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Check first entry
	if entries[0].Title != "llm-wiki-idea" {
		t.Errorf("entry[0].Title = %q", entries[0].Title)
	}
	if entries[0].Type != PageTypeSummary {
		t.Errorf("entry[0].Type = %q", entries[0].Type)
	}

	// Check entity entries
	if entries[1].Title != "karpathy" {
		t.Errorf("entry[1].Title = %q", entries[1].Title)
	}
	if entries[1].Type != PageTypeEntity {
		t.Errorf("entry[1].Type = %q", entries[1].Type)
	}

	// Check synthesis entry
	if entries[3].Title != "design-philosophy" {
		t.Errorf("entry[3].Title = %q", entries[3].Title)
	}
}

func TestPageTypeSection(t *testing.T) {
	tests := []struct {
		pt       PageType
		expected string
	}{
		{PageTypeSummary, "Summaries"},
		{PageTypeEntity, "Entities"},
		{PageTypeConcept, "Concepts"},
		{PageTypeSynthesis, "Synthesis"},
		{PageType(""), "Other"},
		{PageType("unknown"), "Other"},
	}

	for _, tt := range tests {
		got := pageTypeSection(tt.pt)
		if got != tt.expected {
			t.Errorf("pageTypeSection(%q) = %q, want %q", tt.pt, got, tt.expected)
		}
	}
}
