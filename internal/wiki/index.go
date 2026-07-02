package wiki

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// IndexManager manages both the human-readable index.md and the SQLite FTS5 index.
type IndexManager struct {
	indexPath string // path to index.md
	dbPath    string // path to the SQLite FTS5 database
	db        *sql.DB
}

// IndexEntry represents a single entry in index.md.
type IndexEntry struct {
	Title    string    // page title
	Path     string    // relative path from wiki root
	Type     PageType  // page category
	Summary  string    // one-line description
	Modified time.Time // last modified time
}

// NewIndexManager creates a new IndexManager.
func NewIndexManager(indexPath, dbPath string) *IndexManager {
	return &IndexManager{
		indexPath: indexPath,
		dbPath:    dbPath,
	}
}

// Init initializes the index.md file and FTS5 database.
func (im *IndexManager) Init() error {
	// Create index.md with initial content
	content := `# Wiki Index

> This index is automatically maintained. Last updated: ` + time.Now().Format("2006-01-02") + `

## Summaries

*No summaries yet.*

## Entities

*No entities yet.*

## Concepts

*No concepts yet.*

## Synthesis

*No synthesis pages yet.*
`
	if err := os.WriteFile(im.indexPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing index.md: %w", err)
	}

	// Open FTS5 database and create table
	db, err := sql.Open("sqlite", im.dbPath)
	if err != nil {
		return fmt.Errorf("opening FTS database: %w", err)
	}
	im.db = db

	if err := im.createFTSTable(); err != nil {
		return fmt.Errorf("creating FTS table: %w", err)
	}

	return nil
}

// createFTSTable creates the FTS5 virtual table if it doesn't exist.
func (im *IndexManager) createFTSTable() error {
	_, err := im.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
			path,
			title,
			page_type,
			content,
			tokenize='porter unicode61'
		)
	`)
	return err
}

// open opens the SQLite database if not already open.
func (im *IndexManager) open() error {
	if im.db != nil {
		return nil
	}
	db, err := sql.Open("sqlite", im.dbPath)
	if err != nil {
		return fmt.Errorf("opening FTS database: %w", err)
	}
	im.db = db
	return im.createFTSTable()
}

// AddPage adds a page to both index.md and the FTS5 index.
func (im *IndexManager) AddPage(page *Page) error {
	if err := im.open(); err != nil {
		return err
	}

	// Update FTS5 index
	_, err := im.db.Exec(
		"INSERT INTO pages_fts (path, title, page_type, content) VALUES (?, ?, ?, ?)",
		page.Path, page.Title, string(page.Type), page.Content,
	)
	if err != nil {
		return fmt.Errorf("inserting into FTS: %w", err)
	}

	// Update index.md
	return im.updateIndexMd(page, "add")
}

// AddRawSource indexes a raw source file in FTS5 only (not in index.md).
// Raw sources are searchable via FTS5 but don't appear in the human-readable index.
func (im *IndexManager) AddRawSource(path, title, content string) error {
	if err := im.open(); err != nil {
		return err
	}

	_, err := im.db.Exec(
		"INSERT INTO pages_fts (path, title, page_type, content) VALUES (?, ?, ?, ?)",
		path, title, "raw", content,
	)
	if err != nil {
		return fmt.Errorf("inserting raw source into FTS: %w", err)
	}

	return nil
}

// UpdatePage updates a page in both index.md and the FTS5 index.
func (im *IndexManager) UpdatePage(page *Page) error {
	if err := im.open(); err != nil {
		return err
	}

	// Remove old FTS entry and insert new one
	_, err := im.db.Exec("DELETE FROM pages_fts WHERE path = ?", page.Path)
	if err != nil {
		return fmt.Errorf("deleting old FTS entry: %w", err)
	}

	_, err = im.db.Exec(
		"INSERT INTO pages_fts (path, title, page_type, content) VALUES (?, ?, ?, ?)",
		page.Path, page.Title, string(page.Type), page.Content,
	)
	if err != nil {
		return fmt.Errorf("inserting into FTS: %w", err)
	}

	return im.updateIndexMd(page, "update")
}

// RemovePage removes a page from both index.md and the FTS5 index.
func (im *IndexManager) RemovePage(path string) error {
	if err := im.open(); err != nil {
		return err
	}

	// Remove from FTS5
	_, err := im.db.Exec("DELETE FROM pages_fts WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("deleting from FTS: %w", err)
	}

	// Rebuild index.md by scanning remaining FTS entries
	return im.rebuildIndexMd()
}

// Search performs a full-text search using FTS5 and returns matching paths.
// The query string is passed directly to SQLite FTS5.
func (im *IndexManager) Search(query string, limit int) ([]IndexEntry, error) {
	if err := im.open(); err != nil {
		return nil, err
	}

	rows, err := im.db.Query(
		"SELECT path, title, page_type, rank FROM pages_fts WHERE pages_fts MATCH ? ORDER BY rank LIMIT ?",
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("searching FTS: %w", err)
	}
	defer rows.Close()

	var entries []IndexEntry
	for rows.Next() {
		var entry IndexEntry
		var rank float64
		if err := rows.Scan(&entry.Path, &entry.Title, &entry.Type, &rank); err != nil {
			return nil, fmt.Errorf("scanning FTS result: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// ReadIndexMd reads and parses the current index.md.
func (im *IndexManager) ReadIndexMd() ([]IndexEntry, error) {
	content, err := os.ReadFile(im.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading index.md: %w", err)
	}

	return parseIndexMd(string(content)), nil
}

// Close closes the SQLite database connection.
func (im *IndexManager) Close() error {
	if im.db != nil {
		return im.db.Close()
	}
	return nil
}

// updateIndexMd adds or updates an entry in index.md.
func (im *IndexManager) updateIndexMd(page *Page, operation string) error {
	content, err := os.ReadFile(im.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return im.rebuildIndexMd()
		}
		return fmt.Errorf("reading index.md: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	sectionHeader := "## " + pageTypeSection(page.Type)
	targetLine := fmt.Sprintf("- [[%s]](%s) — %s", page.Title, page.Path, time.Now().Format("2006-01-02"))

	// Find the section and insert the entry
	var newLines []string
	inSection := false
	inserted := false
	noPagesLine := fmt.Sprintf("*No %s yet.*", strings.ToLower(string(page.Type)))

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		newLines = append(newLines, line)

		if strings.HasPrefix(line, sectionHeader) {
			inSection = true
			continue
		}

		if inSection {
			// Check if we need to replace the "No X yet." placeholder
			if strings.TrimSpace(line) == noPagesLine {
				if operation == "add" {
					newLines[len(newLines)-1] = targetLine
					inserted = true
				}
				continue
			}

			// Check if this entry already exists (for update)
			if strings.Contains(line, page.Path) {
				if operation == "update" {
					newLines[len(newLines)-1] = targetLine
					inserted = true
				}
				continue
			}

			// Exit section when we hit the next section header or end of content
			if strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, sectionHeader) {
				if !inserted && operation == "add" {
					newLines[len(newLines)-1] = targetLine
					inserted = true
				}
				inSection = false
			}
		}
	}

	// If we haven't inserted yet and we're still in the section, append at end
	if !inserted && operation == "add" {
		newLines = append(newLines, targetLine)
	}

	// Update the "Last updated" line
	result := strings.Join(newLines, "\n")
	result = strings.Replace(result, "> This index is automatically maintained. Last updated:", "> This index is automatically maintained. Last updated: "+time.Now().Format("2006-01-02"), 1)
	// Fix the double date that might result
	result = strings.Replace(result, time.Now().Format("2006-01-02")+" "+time.Now().Format("2006-01-02"), time.Now().Format("2006-01-02"), 1)

	return os.WriteFile(im.indexPath, []byte(result), 0644)
}

// rebuildIndexMd fully rebuilds index.md from the FTS5 database entries.
func (im *IndexManager) rebuildIndexMd() error {
	if err := im.open(); err != nil {
		return err
	}

	rows, err := im.db.Query("SELECT path, title, page_type FROM pages_fts ORDER BY page_type, title")
	if err != nil {
		return fmt.Errorf("querying FTS for rebuild: %w", err)
	}
	defer rows.Close()

	entries := make(map[PageType][]IndexEntry)
	for rows.Next() {
		var entry IndexEntry
		if err := rows.Scan(&entry.Path, &entry.Title, &entry.Type); err != nil {
			return fmt.Errorf("scanning FTS: %w", err)
		}
		entries[entry.Type] = append(entries[entry.Type], entry)
	}

	var sb strings.Builder
	sb.WriteString("# Wiki Index\n\n")
	sb.WriteString("> This index is automatically maintained. Last updated: " + time.Now().Format("2006-01-02") + "\n\n")

	types := []PageType{PageTypeSummary, PageTypeEntity, PageTypeConcept, PageTypeSynthesis}
	for _, pt := range types {
		sb.WriteString("## " + pageTypeSection(pt) + "\n\n")
		if pages, ok := entries[pt]; ok && len(pages) > 0 {
			// Sort by title
			sort.Slice(pages, func(i, j int) bool {
				return pages[i].Title < pages[j].Title
			})
			for _, p := range pages {
				sb.WriteString(fmt.Sprintf("- [[%s]](%s) — %s\n", p.Title, p.Path, time.Now().Format("2006-01-02")))
			}
		} else {
			sb.WriteString(fmt.Sprintf("*No %s yet.*\n", strings.ToLower(string(pt))))
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(im.indexPath, []byte(sb.String()), 0644)
}

// pageTypeSection returns the heading name for a given page type in index.md.
func pageTypeSection(pt PageType) string {
	switch pt {
	case PageTypeSummary:
		return "Summaries"
	case PageTypeEntity:
		return "Entities"
	case PageTypeConcept:
		return "Concepts"
	case PageTypeSynthesis:
		return "Synthesis"
	default:
		return "Other"
	}
}

// parseIndexMd parses index.md content into IndexEntry slices.
func parseIndexMd(content string) []IndexEntry {
	var entries []IndexEntry
	lines := strings.Split(content, "\n")
	var currentType PageType

	for _, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "## Summaries"):
			currentType = PageTypeSummary
		case strings.HasPrefix(line, "## Entities"):
			currentType = PageTypeEntity
		case strings.HasPrefix(line, "## Concepts"):
			currentType = PageTypeConcept
		case strings.HasPrefix(line, "## Synthesis"):
			currentType = PageTypeSynthesis
		case strings.HasPrefix(line, "- [["):
			// Parse: - [[Title]](path) — date
			entry := IndexEntry{Type: currentType}
			if start := strings.Index(line, "[["); start >= 0 {
				if end := strings.Index(line[start:], "]]"); end >= 0 {
					entry.Title = line[start+2 : start+end]
				}
			}
			if start := strings.Index(line, "]("); start >= 0 {
				if end := strings.Index(line[start:], ")"); end >= 0 {
					entry.Path = line[start+2 : start+end]
				}
			}
			entries = append(entries, entry)
		}
	}

	return entries
}
