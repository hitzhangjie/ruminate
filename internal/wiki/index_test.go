package wiki

import (
	"math"
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

// --- Vector storage tests ---

func TestSerializeDeserializeVector(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	data := serializeVector(original)
	restored, err := deserializeVector(data)
	if err != nil {
		t.Fatalf("deserializeVector error: %v", err)
	}
	if len(restored) != len(original) {
		t.Fatalf("length mismatch: %d vs %d", len(restored), len(original))
	}
	for i := range original {
		if math.Abs(float64(restored[i]-original[i])) > 1e-6 {
			t.Errorf("mismatch at [%d]: got %f, want %f", i, restored[i], original[i])
		}
	}
}

func TestDeserializeVector_TooShort(t *testing.T) {
	_, err := deserializeVector([]byte{0x01}) // only 1 byte, need at least 2
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestDeserializeVector_DimensionMismatch(t *testing.T) {
	// Header claims 10 dimensions (0x000A), but the total data is only 6 bytes.
	// 10 dims require 2 + 10*4 = 42 bytes, so deserialization should fail.
	data := []byte{0x0A, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err := deserializeVector(data)
	if err == nil {
		t.Error("expected dimension mismatch error")
	}
}

func TestCosineSimilarity_Identity(t *testing.T) {
	vec := []float32{1, 2, 3}
	sim := cosineSimilarity(vec, vec)
	if math.Abs(float64(sim-1.0)) > 1e-6 {
		t.Errorf("identity similarity should be 1.0, got %f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim := cosineSimilarity(a, b)
	if math.Abs(float64(sim-0.0)) > 1e-6 {
		t.Errorf("orthogonal similarity should be 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	sim := cosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("zero vector similarity should be 0, got %f", sim)
	}
}

func TestCosineSimilarity_DimensionMismatch(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	sim := cosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("dimension mismatch similarity should be 0, got %f", sim)
	}
}

func TestVectorStoreSearchDelete(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	dbPath := filepath.Join(dir, "fts.db")

	im := NewIndexManager(indexPath, dbPath)
	if err := im.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer im.Close()

	// Add pages to FTS5 (required for vector search JOIN)
	pages := []*Page{
		{Title: "java-install", Path: "wiki/summaries/java-install.md", Type: PageTypeSummary, Content: "How to install Java"},
		{Title: "python-install", Path: "wiki/summaries/python-install.md", Type: PageTypeSummary, Content: "How to install Python"},
		{Title: "go-install", Path: "wiki/summaries/go-install.md", Type: PageTypeSummary, Content: "How to install Go"},
	}
	for _, p := range pages {
		if err := im.AddPage(p); err != nil {
			t.Fatalf("AddPage error: %v", err)
		}
	}

	// Store vectors: java page should be closest to java query
	// Simplified: use sparse-like vectors for predictable results
	storeVec := func(path string, vals ...float32) {
		if err := im.StoreVector(path, vals); err != nil {
			t.Fatalf("StoreVector(%s) error: %v", path, err)
		}
	}
	storeVec("wiki/summaries/java-install.md", 1.0, 0.0, 0.0)
	storeVec("wiki/summaries/python-install.md", 0.0, 1.0, 0.0)
	storeVec("wiki/summaries/go-install.md", 0.0, 0.0, 1.0)

	// Search: query vector close to java
	queryVec := []float32{0.9, 0.1, 0.0}
	results, err := im.SearchByVector(queryVec, 2)
	if err != nil {
		t.Fatalf("SearchByVector error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// java-install should be top result
	if results[0].Title != "java-install" {
		t.Errorf("top result should be java-install, got %s", results[0].Title)
	}

	// Delete java vector
	if err := im.DeleteVector("wiki/summaries/java-install.md"); err != nil {
		t.Fatalf("DeleteVector error: %v", err)
	}
	results, _ = im.SearchByVector(queryVec, 3)
	for _, r := range results {
		if r.Title == "java-install" {
			t.Error("deleted vector should not appear in search results")
		}
	}
}
