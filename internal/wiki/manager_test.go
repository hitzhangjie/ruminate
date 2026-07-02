package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManager_Init(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	// Verify all directories were created
	dirs := []string{
		filepath.Join(dir, "raw"),
		filepath.Join(dir, "wiki", "summaries"),
		filepath.Join(dir, "wiki", "entities"),
		filepath.Join(dir, "wiki", "concepts"),
		filepath.Join(dir, "wiki", "synthesis"),
		filepath.Join(dir, ".ruminate"),
	}

	for _, d := range dirs {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			t.Errorf("directory should exist: %s", d)
		}
	}

	// Verify schema.md was created
	schemaPath := filepath.Join(dir, "schema.md")
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		t.Errorf("schema.md should exist")
	}

	// Verify index.md was created
	indexPath := filepath.Join(dir, "index.md")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("index.md should exist")
	}

	// Verify log.md was created
	logPath := filepath.Join(dir, "log.md")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("log.md should exist")
	}

	// Verify .git was initialized
	gitPath := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		t.Errorf(".git should exist")
	}
}

func TestManager_Init_Idempotent(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("first Init() error: %v", err)
	}
	defer mgr.Close()

	// Second init should not error
	if err := mgr.Init(); err != nil {
		t.Errorf("second Init() should succeed, got: %v", err)
	}
}

func TestManager_AddSource(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	content := []byte("# My Article\n\nThis is a test article.")

	// Add a source with type "article"
	_, err := mgr.AddSource("article", "My Article", content)
	if err != nil {
		t.Fatalf("AddSource(article) error: %v", err)
	}

	// Verify the file exists (sanitizeFilename preserves spaces, only replaces special chars)
	articlePath := filepath.Join(dir, "raw", "article", "My Article.md")
	if _, err := os.Stat(articlePath); os.IsNotExist(err) {
		t.Errorf("source file should exist: %s", articlePath)
	}

	// Verify content
	readContent, err := os.ReadFile(articlePath)
	if err != nil {
		t.Fatalf("reading source file: %v", err)
	}
	if string(readContent) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(readContent), string(content))
	}

	// Add another source with a different type "paper"
	paperContent := []byte("# My Paper\n\nPaper content.")
	_, err = mgr.AddSource("paper", "My Paper", paperContent)
	if err != nil {
		t.Fatalf("AddSource(paper) error: %v", err)
	}

	// Verify paper directory was created on-demand
	paperPath := filepath.Join(dir, "raw", "paper", "My Paper.md")
	if _, err := os.Stat(paperPath); os.IsNotExist(err) {
		t.Errorf("paper source file should exist: %s", paperPath)
	}

	// Add second article — same type, different title
	article2Content := []byte("# Second Article\n\nMore content.")
	_, err = mgr.AddSource("article", "Second Article", article2Content)
	if err != nil {
		t.Fatalf("AddSource(article, second) error: %v", err)
	}

	article2Path := filepath.Join(dir, "raw", "article", "Second Article.md")
	if _, err := os.Stat(article2Path); os.IsNotExist(err) {
		t.Errorf("second article file should exist: %s", article2Path)
	}
}

func TestManager_AddSource_Validation(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	// Empty sourceType should error
	if _, err := mgr.AddSource("", "Title", []byte("content")); err == nil {
		t.Error("AddSource with empty sourceType should error")
	}

	// Empty title should error
	if _, err := mgr.AddSource("article", "", []byte("content")); err == nil {
		t.Error("AddSource with empty title should error")
	}
}

func TestManager_ListSources(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	// Add sources of different types
	_, _ = mgr.AddSource("article", "Article One", []byte("# One"))
	_, _ = mgr.AddSource("article", "Article Two", []byte("# Two"))
	_, _ = mgr.AddSource("paper", "Paper One", []byte("# Paper"))

	// List all
	all, err := mgr.ListSources("")
	if err != nil {
		t.Fatalf("ListSources() error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 sources total, got %d: %v", len(all), all)
	}

	// List by type
	articles, err := mgr.ListSources("article")
	if err != nil {
		t.Fatalf("ListSources(article) error: %v", err)
	}
	if len(articles) != 2 {
		t.Errorf("expected 2 articles, got %d", len(articles))
	}

	papers, err := mgr.ListSources("paper")
	if err != nil {
		t.Fatalf("ListSources(paper) error: %v", err)
	}
	if len(papers) != 1 {
		t.Errorf("expected 1 paper, got %d", len(papers))
	}

	// List nonexistent type should return empty, not error
	empty, err := mgr.ListSources("nonexistent")
	if err != nil {
		t.Fatalf("ListSources(nonexistent) error: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 sources for nonexistent type, got %d", len(empty))
	}
}

func TestManager_IsInitialized(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if mgr.IsInitialized() {
		t.Error("should not be initialized before Init()")
	}

	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	if !mgr.IsInitialized() {
		t.Error("should be initialized after Init()")
	}
}

func TestManager_CreateAndRead(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	content := `# Andrej Karpathy

Andrej Karpathy is an AI researcher and educator.

He previously worked at [[tesla]] and [[openai]].

## Key Contributions

- [[deep-learning]] education
- [[llm]] research
`

	page, err := mgr.Create("Andrej Karpathy", PageTypeEntity, content)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Verify page metadata
	if page.Title != "Andrej Karpathy" {
		t.Errorf("Title = %q", page.Title)
	}
	if page.Type != PageTypeEntity {
		t.Errorf("Type = %q", page.Type)
	}
	if !strings.Contains(page.Path, "wiki/entities") {
		t.Errorf("Path should contain wiki/entities: %q", page.Path)
	}

	// Verify parsed WikiLinks
	expectedLinks := []string{"tesla", "openai", "deep-learning", "llm"}
	if len(page.Links) != len(expectedLinks) {
		t.Errorf("expected %d links, got %d: %v", len(expectedLinks), len(page.Links), page.Links)
	}

	// Read it back
	readPage, err := mgr.Read("Andrej Karpathy", PageTypeEntity)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	if readPage.Content != content {
		t.Errorf("read content differs from written content")
	}
	if readPage.Title != page.Title {
		t.Errorf("Title mismatch: got %q, want %q", readPage.Title, page.Title)
	}
}

func TestManager_Create_Duplicate(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	_, err := mgr.Create("Test Page", PageTypeSummary, "# Test")
	if err != nil {
		t.Fatalf("first Create() error: %v", err)
	}

	_, err = mgr.Create("Test Page", PageTypeSummary, "# Test 2")
	if err == nil {
		t.Error("duplicate create should error")
	}
}

func TestManager_Update(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	original := "# Original Content\n\nSee [[page-a]]."
	_, err := mgr.Create("Test Page", PageTypeSummary, original)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	updated := "# Updated Content\n\nSee [[page-b]]."
	page, err := mgr.Update("Test Page", PageTypeSummary, updated)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	if page.Content != updated {
		t.Errorf("content not updated")
	}

	if len(page.Links) != 1 || page.Links[0] != "page-b" {
		t.Errorf("links should reflect updated content: %v", page.Links)
	}

	// Verify on disk
	readPage, err := mgr.Read("Test Page", PageTypeSummary)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if readPage.Content != updated {
		t.Errorf("on-disk content doesn't match")
	}
}

func TestManager_Update_Nonexistent(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	_, err := mgr.Update("Nonexistent", PageTypeEntity, "# Content")
	if err == nil {
		t.Error("updating nonexistent page should error")
	}
}

func TestManager_Delete(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	_, err := mgr.Create("Temp Page", PageTypeConcept, "# Temporary")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	if err := mgr.Delete("Temp Page", PageTypeConcept); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// Verify it's gone
	_, err = mgr.Read("Temp Page", PageTypeConcept)
	if err == nil {
		t.Error("Read() should error after delete")
	}
}

func TestManager_Delete_Nonexistent(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	err := mgr.Delete("Nonexistent", PageTypeEntity)
	if err == nil {
		t.Error("deleting nonexistent page should error")
	}
}

func TestManager_List(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	// Create pages of different types
	pages := []struct {
		title    string
		pageType PageType
		content  string
	}{
		{"Entity One", PageTypeEntity, "# Entity One"},
		{"Entity Two", PageTypeEntity, "# Entity Two"},
		{"Concept One", PageTypeConcept, "# Concept One"},
		{"Summary One", PageTypeSummary, "# Summary One"},
	}

	for _, p := range pages {
		if _, err := mgr.Create(p.title, p.pageType, p.content); err != nil {
			t.Fatalf("Create(%q) error: %v", p.title, err)
		}
	}

	// List all
	all, err := mgr.List("")
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("expected 4 pages total, got %d", len(all))
	}

	// List by type
	entities, err := mgr.List(PageTypeEntity)
	if err != nil {
		t.Fatalf("List(entities) error: %v", err)
	}
	if len(entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(entities))
	}

	concepts, err := mgr.List(PageTypeConcept)
	if err != nil {
		t.Fatalf("List(concepts) error: %v", err)
	}
	if len(concepts) != 1 {
		t.Errorf("expected 1 concept, got %d", len(concepts))
	}
}

func TestManager_ReadByPath(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	content := "# Test Content"
	page, err := mgr.Create("Test Title", PageTypeEntity, content)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	readPage, err := mgr.ReadByPath(page.Path)
	if err != nil {
		t.Fatalf("ReadByPath() error: %v", err)
	}

	if readPage.Content != content {
		t.Errorf("content mismatch: got %q, want %q", readPage.Content, content)
	}
}

func TestManager_ReadByPath_Nonexistent(t *testing.T) {
	dir := t.TempDir()

	mgr := NewManager(dir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer mgr.Close()

	_, err := mgr.ReadByPath("wiki/entities/nonexistent.md")
	if err == nil {
		t.Error("ReadByPath should error for nonexistent path")
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with/slash", "with-slash"},
		{"with:colon", "with-colon"},
		{"with*star", "with-star"},
		{"with?question", "with-question"},
		{"with\"quote\"", "with-quote-"},
		{"with<angle>", "with-angle-"},
		{"with|pipe", "with-pipe"},
		{"with\\backslash", "with-backslash"},
		{"multiple:*?chars", "multiple---chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
