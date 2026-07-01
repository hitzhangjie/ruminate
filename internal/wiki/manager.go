package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/gitwrap"
)

// PageType classifies a wiki page.
type PageType string

const (
	PageTypeSummary   PageType = "summaries"
	PageTypeEntity    PageType = "entities"
	PageTypeConcept   PageType = "concepts"
	PageTypeSynthesis PageType = "synthesis"
)

// Page represents a single wiki page.
type Page struct {
	// Title is the page title (derived from filename without extension).
	Title string
	// Path is the relative path from the wiki root (e.g., "wiki/entities/karpathy.md").
	Path string
	// Type is the page category.
	Type PageType
	// Content is the full Markdown content.
	Content string
	// Links are the WikiLink targets found in the content.
	Links []string
}

// Manager handles wiki page CRUD and directory structure.
type Manager struct {
	root     string        // wiki root directory path
	wikiDir  string        // wiki/ subdirectory
	rawDir   string        // raw/ subdirectory
	git      *gitwrap.Git  // git wrapper for version control
	index    *IndexManager // index manager
	log      *LogManager   // log manager
}

// NewManager creates a wiki manager for the given root path.
// If the wiki directory does not exist, Init() must be called to create it.
func NewManager(root string) *Manager {
	return &Manager{
		root:    root,
		wikiDir: filepath.Join(root, "wiki"),
		rawDir:  filepath.Join(root, "raw"),
		git:     gitwrap.New(root),
	}
}

// Root returns the wiki root directory path.
func (m *Manager) Root() string { return m.root }

// WikiDir returns the path to the wiki/ subdirectory.
func (m *Manager) WikiDir() string { return m.wikiDir }

// RawDir returns the path to the raw/ subdirectory.
func (m *Manager) RawDir() string { return m.rawDir }

// Init creates the full wiki directory structure and initializes git.
// Returns nil if the structure already exists.
func (m *Manager) Init() error {
	dirs := []string{
		m.root,
		m.rawDir,
		m.wikiDir,
		filepath.Join(m.wikiDir, "summaries"),
		filepath.Join(m.wikiDir, "entities"),
		filepath.Join(m.wikiDir, "concepts"),
		filepath.Join(m.wikiDir, "synthesis"),
		filepath.Join(m.root, ".ruminate"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Initialize git repository
	if err := m.git.Init(); err != nil {
		return fmt.Errorf("initializing git: %w", err)
	}

	// Initialize index manager
	indexPath := filepath.Join(m.root, "index.md")
	m.index = NewIndexManager(indexPath, filepath.Join(m.root, ".ruminate", "fts.db"))

	// Initialize log manager
	logPath := filepath.Join(m.root, "log.md")
	m.log = NewLogManager(logPath)

	// Write schema.md if it doesn't exist
	schemaPath := filepath.Join(m.root, "schema.md")
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		if err := os.WriteFile(schemaPath, []byte(defaultSchema), 0644); err != nil {
			return fmt.Errorf("writing schema.md: %w", err)
		}
	}

	// Write initial index.md if it doesn't exist
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if err := m.index.Init(); err != nil {
			return fmt.Errorf("initializing index.md: %w", err)
		}
	}

	// Write initial log.md if it doesn't exist
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		if err := m.log.Init(); err != nil {
			return fmt.Errorf("initializing log.md: %w", err)
		}
	}

	return nil
}

// IsInitialized returns true if the wiki directory structure exists.
func (m *Manager) IsInitialized() bool {
	_, err := os.Stat(m.wikiDir)
	return err == nil
}

// AddSource saves raw source material to raw/<sourceType>/<filename>.md.
// The raw/<sourceType>/ directory is created on-demand if it doesn't exist.
// sourceType is a user-defined label: "article", "blog", "paper", "note", "idea", etc.
// title is used as the filename (sanitized).
func (m *Manager) AddSource(sourceType string, title string, content []byte) error {
	if sourceType == "" {
		return fmt.Errorf("sourceType is required")
	}
	if title == "" {
		return fmt.Errorf("title is required")
	}

	dir := filepath.Join(m.rawDir, sourceType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating source directory %s: %w", dir, err)
	}

	filename := sanitizeFilename(title) + ".md"
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("writing source file: %w", err)
	}

	return nil
}

// ListSources returns the relative paths of all raw source files,
// optionally filtered by sourceType. If sourceType is empty, all sources are returned.
func (m *Manager) ListSources(sourceType string) ([]string, error) {
	searchDir := m.rawDir
	if sourceType != "" {
		searchDir = filepath.Join(m.rawDir, sourceType)
	}

	var sources []string
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip directories that don't exist (e.g., unused sourceType)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		relPath, _ := filepath.Rel(m.root, path)
		sources = append(sources, filepath.ToSlash(relPath))
		return nil
	})

	return sources, err
}

// Create creates a new wiki page and writes it to disk.
func (m *Manager) Create(title string, pageType PageType, content string) (*Page, error) {
	m.ensureComponents()

	filename := sanitizeFilename(title) + ".md"
	dir := filepath.Join(m.wikiDir, string(pageType))
	path := filepath.Join(dir, filename)

	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("page already exists: %s", path)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	page := &Page{
		Title:   title,
		Path:    filepath.Join("wiki", string(pageType), filename),
		Type:    pageType,
		Content: content,
		Links:   ParseWikiLinks(content),
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("writing page: %w", err)
	}

	// Update index
	if err := m.index.AddPage(page); err != nil {
		return nil, fmt.Errorf("updating index: %w", err)
	}

	// Write log entry
	if err := m.log.Append("create", pageType, title, ""); err != nil {
		return nil, fmt.Errorf("writing log: %w", err)
	}

	return page, nil
}

// Read reads a wiki page from disk.
func (m *Manager) Read(title string, pageType PageType) (*Page, error) {
	m.ensureComponents()

	filename := sanitizeFilename(title) + ".md"
	dir := filepath.Join(m.wikiDir, string(pageType))
	path := filepath.Join(dir, filename)

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("page not found: %s/%s", pageType, title)
		}
		return nil, fmt.Errorf("reading page: %w", err)
	}

	return &Page{
		Title:   title,
		Path:    filepath.Join("wiki", string(pageType), filename),
		Type:    pageType,
		Content: string(content),
		Links:   ParseWikiLinks(string(content)),
	}, nil
}

// ReadByPath reads a wiki page using its relative path from wiki root.
func (m *Manager) ReadByPath(relPath string) (*Page, error) {
	m.ensureComponents()

	fullPath := filepath.Join(m.root, relPath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("page not found: %s", relPath)
		}
		return nil, fmt.Errorf("reading page: %w", err)
	}

	// Derive title and type from path
	parts := strings.SplitN(filepath.ToSlash(relPath), "/", 3)
	title := strings.TrimSuffix(filepath.Base(relPath), ".md")
	pageType := PageTypeSummary // default
	if len(parts) >= 3 && parts[0] == "wiki" {
		pageType = PageType(parts[1])
	}

	return &Page{
		Title:   title,
		Path:    relPath,
		Type:    pageType,
		Content: string(content),
		Links:   ParseWikiLinks(string(content)),
	}, nil
}

// Update updates an existing wiki page's content.
func (m *Manager) Update(title string, pageType PageType, newContent string) (*Page, error) {
	m.ensureComponents()

	_, err := m.Read(title, pageType)
	if err != nil {
		return nil, err
	}

	filename := sanitizeFilename(title) + ".md"
	dir := filepath.Join(m.wikiDir, string(pageType))
	path := filepath.Join(dir, filename)

	page := &Page{
		Title:   title,
		Path:    filepath.Join("wiki", string(pageType), filename),
		Type:    pageType,
		Content: newContent,
		Links:   ParseWikiLinks(newContent),
	}

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("writing page: %w", err)
	}

	// Update index
	if err := m.index.UpdatePage(page); err != nil {
		return nil, fmt.Errorf("updating index: %w", err)
	}

	// Write log entry
	if err := m.log.Append("update", pageType, title, ""); err != nil {
		return nil, fmt.Errorf("writing log: %w", err)
	}

	return page, nil
}

// Delete removes a wiki page from disk.
func (m *Manager) Delete(title string, pageType PageType) error {
	m.ensureComponents()

	filename := sanitizeFilename(title) + ".md"
	dir := filepath.Join(m.wikiDir, string(pageType))
	path := filepath.Join(dir, filename)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("page not found: %s/%s", pageType, title)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting page: %w", err)
	}

	// Remove from index
	relPath := filepath.Join("wiki", string(pageType), filename)
	if err := m.index.RemovePage(relPath); err != nil {
		return fmt.Errorf("updating index: %w", err)
	}

	// Write log entry
	if err := m.log.Append("delete", pageType, title, ""); err != nil {
		return fmt.Errorf("writing log: %w", err)
	}

	return nil
}

// List returns all wiki pages, optionally filtered by type.
// If pageType is empty, returns all pages.
func (m *Manager) List(pageType PageType) ([]*Page, error) {
	m.ensureComponents()

	var searchDir string
	if pageType == "" {
		searchDir = m.wikiDir
	} else {
		searchDir = filepath.Join(m.wikiDir, string(pageType))
	}

	var pages []*Page
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		// Determine page type from directory structure
		relPath, _ := filepath.Rel(m.root, path)
		relPath = filepath.ToSlash(relPath)
		parts := strings.SplitN(relPath, "/", 3)
		var pt PageType
		if len(parts) >= 3 && parts[0] == "wiki" {
			pt = PageType(parts[1])
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		pages = append(pages, &Page{
			Title:   strings.TrimSuffix(info.Name(), ".md"),
			Path:    relPath,
			Type:    pt,
			Content: string(content),
			Links:   ParseWikiLinks(string(content)),
		})
		return nil
	})

	return pages, err
}

// Index returns the index manager, initializing it if needed.
func (m *Manager) Index() *IndexManager {
	m.ensureComponents()
	return m.index
}

// Log returns the log manager, initializing it if needed.
func (m *Manager) Log() *LogManager {
	m.ensureComponents()
	return m.log
}

// Git returns the git wrapper.
func (m *Manager) Git() *gitwrap.Git {
	return m.git
}

// Close closes the wiki manager and releases resources (e.g., SQLite connection).
func (m *Manager) Close() error {
	if m.index != nil {
		return m.index.Close()
	}
	return nil
}

// ensureComponents initializes index and log managers if they are nil.
// This allows callers to use an uninitialized Manager for read operations.
func (m *Manager) ensureComponents() {
	if m.index == nil {
		m.index = NewIndexManager(
			filepath.Join(m.root, "index.md"),
			filepath.Join(m.root, ".ruminate", "fts.db"),
		)
	}
	if m.log == nil {
		m.log = NewLogManager(filepath.Join(m.root, "log.md"))
	}
}

// sanitizeFilename converts a title to a safe filename.
func sanitizeFilename(title string) string {
	// Replace characters that are problematic in filenames
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
	)
	return replacer.Replace(title)
}

// defaultSchema is the default content for schema.md.
const defaultSchema = `# Wiki Schema

This file defines the structure and writing conventions for this wiki.

## Page Types

- **summary**: Source material summaries
- **entity**: Entities (people, events, terms, etc.)
- **concept**: Concepts / themes
- **synthesis**: Synthesis / comparison / review pages

## Naming

- summary: "{source_title}.md"
- entity: "{entity_name}.md"
- concept: "{concept_name}.md"
- synthesis: "{topic}.md"

## Linking

- style: "[[page-name]]" (WikiLink format)
- bidirectional: true (backlinks are automatically maintained)

## Ingest Rules

- max_summary_length: 500
- extract_entities: true
- extract_concepts: true
- update_existing: true

## Lint Rules

- check_contradictions: true
- check_orphans: true
- check_staleness_days: 90
- suggest_new_pages: true
`
