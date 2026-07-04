package wiki

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

// IndexManager manages both the human-readable index.md and the SQLite FTS5 index.
//
// IMPORTANT: pages_fts (SQLite FTS5) is the source of truth for SEARCH. Both `find` and
// `ask` commands query pages_fts to locate relevant pages. index.md is a DERIVED
// human-readable directory — it is rebuilt from pages_fts (see rebuildIndexMd), never
// the other way around. Raw sources are indexed only in pages_fts and do NOT appear
// in index.md at all.
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

*No synthesis yet.*
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

	if err := im.createVectorTable(); err != nil {
		return fmt.Errorf("creating vector table: %w", err)
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

// createVectorTable creates the page_vectors table for embedding storage.
func (im *IndexManager) createVectorTable() error {
	_, err := im.db.Exec(`
		CREATE TABLE IF NOT EXISTS page_vectors (
			path TEXT PRIMARY KEY,
			embedding BLOB NOT NULL,
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	return err
}

// serializeVector encodes a []float32 vector into a compact binary format:
// [2 bytes: dimension as uint16 LE][N*4 bytes: each float32 in LE].
func serializeVector(vec []float32) []byte {
	buf := make([]byte, 2+len(vec)*4)
	binary.LittleEndian.PutUint16(buf[:2], uint16(len(vec)))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[2+i*4:], math.Float32bits(v))
	}
	return buf
}

// deserializeVector decodes a binary-encoded vector back to []float32.
// Returns an error if the data is too short or the declared dimension doesn't
// match the payload length.
func deserializeVector(data []byte) ([]float32, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("vector data too short: %d bytes", len(data))
	}
	dim := binary.LittleEndian.Uint16(data[:2])
	expected := 2 + int(dim)*4
	if len(data) != expected {
		return nil, fmt.Errorf("vector dimension mismatch: declared %d, got %d bytes", dim, len(data))
	}
	vec := make([]float32, dim)
	for i := uint16(0); i < dim; i++ {
		bits := binary.LittleEndian.Uint32(data[2+i*4:])
		vec[i] = math.Float32frombits(bits)
	}
	return vec, nil
}

// cosineSimilarity returns the cosine similarity between two vectors.
// Returns 0 if either vector is zero-length or dimensions don't match.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// StoreVector inserts or replaces a page's embedding vector.
func (im *IndexManager) StoreVector(path string, vec []float32) error {
	if err := im.open(); err != nil {
		return err
	}
	encoded := serializeVector(vec)
	_, err := im.db.Exec(
		"INSERT OR REPLACE INTO page_vectors (path, embedding, updated_at) VALUES (?, ?, datetime('now'))",
		path, encoded,
	)
	if err != nil {
		return fmt.Errorf("storing vector: %w", err)
	}
	return nil
}

// DeleteVector removes a page's embedding vector.
func (im *IndexManager) DeleteVector(path string) error {
	if err := im.open(); err != nil {
		return err
	}
	_, err := im.db.Exec("DELETE FROM page_vectors WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("deleting vector: %w", err)
	}
	return nil
}

// SearchByVector performs brute-force cosine similarity search over all
// stored embeddings and returns the top-N matching pages.
func (im *IndexManager) SearchByVector(queryVec []float32, limit int) ([]SearchResult, error) {
	if err := im.open(); err != nil {
		return nil, err
	}

	rows, err := im.db.Query(`
		SELECT pv.path, pv.embedding, p.title, p.page_type
		FROM page_vectors pv
		JOIN pages_fts p ON pv.path = p.path
	`)
	if err != nil {
		return nil, fmt.Errorf("querying vectors: %w", err)
	}
	defer rows.Close()

	type scored struct {
		SearchResult
		score float32
	}
	var scoredList []scored

	for rows.Next() {
		var path, title, pageType string
		var embData []byte
		if err := rows.Scan(&path, &embData, &title, &pageType); err != nil {
			return nil, fmt.Errorf("scanning vector row: %w", err)
		}
		vec, err := deserializeVector(embData)
		if err != nil {
			continue // skip corrupted vectors
		}
		sim := cosineSimilarity(queryVec, vec)
		scoredList = append(scoredList, scored{
			SearchResult: SearchResult{
				IndexEntry: IndexEntry{
					Path:  path,
					Title: title,
					Type:  PageType(pageType),
				},
			},
			score: sim,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by descending similarity
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	if limit > len(scoredList) {
		limit = len(scoredList)
	}
	results := make([]SearchResult, limit)
	for i := 0; i < limit; i++ {
		results[i] = scoredList[i].SearchResult
	}

	return results, nil
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
	if err := im.createFTSTable(); err != nil {
		return err
	}
	return im.createVectorTable()
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

// SearchResult extends IndexEntry with a relevance-ranked snippet from FTS5.
type SearchResult struct {
	IndexEntry
	Snippet string  // highlighted snippet from FTS5 snippet()
	Rank    float64 // BM25 rank score (lower = more relevant)
}

// SearchWithSnippets performs a full-text search using FTS5 AND semantics and
// returns results with highlighted snippets via FTS5's snippet() function.
//
// It uses the query as-is — FTS5's unicode61 tokenizer applies implicit AND
// between terms. Callers that need OR fallback should call searchWithSnippets
// with a query built by toFTS5OrQuery.
//
// The snippet function wraps matching terms in <b>...</b> tags. The caller
// can render these as ANSI-bold for terminal output or HTML for web display.
func (im *IndexManager) SearchWithSnippets(query string, limit int) ([]SearchResult, error) {
	if err := im.open(); err != nil {
		return nil, err
	}

	return im.searchWithSnippets(query, limit)
}

// searchWithSnippets is the internal implementation that executes the FTS5 query.
func (im *IndexManager) searchWithSnippets(query string, limit int) ([]SearchResult, error) {

	// snippet(pages_fts, 3, '<b>', '</b>', '...', 32)
	//  - 3: column index of the content column (0=path, 1=title, 2=page_type, 3=content)
	//  - '<b>', '</b>': highlight markers
	//  - '...': ellipsis for truncated text
	//  - 32: max tokens per snippet
	sql := `SELECT path, title, page_type, snippet(pages_fts, 3, '<b>', '</b>', '...', 32) AS snippet, rank
		FROM pages_fts
		WHERE pages_fts MATCH ?
		ORDER BY rank
		LIMIT ?`

	rows, err := im.db.Query(sql, query, limit)
	if err != nil {
		return nil, fmt.Errorf("searching FTS with snippets: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Path, &r.Title, &r.Type, &r.Snippet, &r.Rank); err != nil {
			return nil, fmt.Errorf("scanning FTS snippet result: %w", err)
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

// toFTS5OrQuery transforms a natural-language query into an OR-connected
// FTS5 query. It splits on whitespace/punctuation and Unicode script
// boundaries (e.g. Latin↔CJK), drops very short tokens, and joins with
// " OR " so that matching ANY token is sufficient.
//
// Splitting on script boundaries is essential for mixed-language queries
// like "golang垃圾回收" (no space between English and Chinese). FTS5's
// unicode61 tokenizer splits at script boundaries during indexing, so a
// quoted phrase like "golang垃圾回收" would never match. By splitting into
// "golang" OR "垃圾回收", each token can match independently.
func toFTS5OrQuery(query string) string {
	words := splitForFTS5Query(query)

	var filtered []string
	for _, w := range words {
		// Keep tokens of at least 2 characters to avoid matching noise.
		if len(w) >= 2 {
			filtered = append(filtered, `"`+w+`"`)
		}
	}

	if len(filtered) == 0 {
		return ""
	}

	return strings.Join(filtered, " OR ")
}

// splitForFTS5Query splits a query string into tokens mimicking FTS5's
// unicode61 tokenizer behavior: it splits on whitespace, punctuation,
// and at script boundaries between CJK and non-CJK characters.
func splitForFTS5Query(query string) []string {
	var words []string
	var current []rune
	var inCJK bool
	var hasRunes bool

	flush := func() {
		if hasRunes {
			words = append(words, string(current))
			current = nil
			hasRunes = false
		}
	}

	for _, r := range query {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			flush()
			inCJK = false
			continue
		}

		runeIsCJK := isCJK(r)
		if hasRunes && runeIsCJK != inCJK {
			flush()
		}

		inCJK = runeIsCJK
		hasRunes = true
		current = append(current, r)
	}

	flush()

	return words
}

// isCJK reports whether r is a CJK character (Han, Hiragana, Katakana, or Hangul).
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
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
					// Insert entry before the next section header (don't overwrite it)
					newLines[len(newLines)-1] = targetLine
					newLines = append(newLines, line)
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
