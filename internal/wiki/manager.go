package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/gitwrap"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/trace"
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
	root        string                // wiki root directory path
	wikiDir     string                // wiki/ subdirectory
	rawDir      string                // raw/ subdirectory
	git         *gitwrap.Git          // git wrapper for version control
	index       *IndexManager         // index manager
	log         *LogManager           // log manager
	embedder    llm.EmbeddingProvider // optional embedding provider for semantic search
	llmProvider llm.LLMProvider       // LLM provider for inference
	llmCfg      config.LLMConfig      // LLM configuration
	tracer      *trace.Tracer         // pipeline step recorder
}

// SetEmbeddingProvider sets the embedding provider used to compute vectors
// for pages created/updated via this Manager. Pass nil to disable embeddings.
func (m *Manager) SetEmbeddingProvider(ep llm.EmbeddingProvider) {
	m.embedder = ep
}

// SetTracer attaches a tracer for pipeline observability. Pass nil to disable.
func (m *Manager) SetTracer(tr *trace.Tracer) {
	m.tracer = tr
}

// NewManager creates a wiki manager from the given configuration.
// The embedder is automatically initialized from cfg.Embedding; a missing
// or unreachable embedding provider is treated as non-fatal (embedder stays nil).
// If the wiki directory does not exist, Init() must be called to create it.
func NewManager(cfg *config.Config) *Manager {
	root := config.ExpandPath(cfg.WikiPath)
	m := &Manager{
		root:    root,
		wikiDir: filepath.Join(root, "wiki"),
		rawDir:  filepath.Join(root, "raw"),
		git:     gitwrap.New(root),
	}

	// Auto-initialize embedder from config. Non-fatal: embedder stays nil
	// if provider is empty or unreachable, keeping wiki operations working.
	if cfg.Embedding.Provider != "" {
		if embedder, err := llm.NewEmbeddingProvider(
			cfg.Embedding.Provider,
			cfg.Embedding.BaseURL,
			cfg.Embedding.Model,
		); err == nil {
			m.embedder = embedder
		}
	}

	// Auto-initialize LLM provider from config. Non-fatal: llmProvider stays
	// nil if provider is empty or unreachable.
	m.llmCfg = cfg.LLM
	if cfg.LLM.Provider != "" {
		if provider, err := llm.NewProvider(
			cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model,
		); err == nil {
			m.llmProvider = provider
		}
	}

	return m
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

// AddSource saves raw source material to raw/<sourceType>/<filename>.md
// and indexes it in FTS5 for full-text search.
// The raw/<sourceType>/ directory is created on-demand if it doesn't exist.
// sourceType is a user-defined label: "article", "paper", "note", "book", etc.
// title is used as the filename (sanitized).
//
// Returns the relative path of the saved source file (from wiki root).
func (m *Manager) AddSource(sourceType string, title string, content []byte) (string, error) {
	if sourceType == "" {
		return "", fmt.Errorf("sourceType is required")
	}
	if title == "" {
		return "", fmt.Errorf("title is required")
	}

	dir := filepath.Join(m.rawDir, sourceType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating source directory %s: %w", dir, err)
	}

	filename := sanitizeFilename(title) + ".md"
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", fmt.Errorf("writing source file: %w", err)
	}

	// Index the raw source in FTS5 for full-text search.
	relPath := filepath.ToSlash(filepath.Join("raw", sourceType, filename))
	m.ensureComponents()
	if err := m.index.AddRawSource(relPath, title, string(content)); err != nil {
		return "", fmt.Errorf("indexing raw source: %w", err)
	}

	// Compute and store embedding for semantic search
	m.computeAndStoreEmbedding(string(content), relPath)

	return relPath, nil
}

// RawSourcePath returns the relative path where a raw source would be stored,
// without actually writing it. Useful for generating links before the file exists.
func (m *Manager) RawSourcePath(sourceType, title string) string {
	return filepath.ToSlash(filepath.Join("raw", sourceType, sanitizeFilename(title)+".md"))
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

	// Compute and store embedding for semantic search
	m.computeAndStoreEmbedding(content, page.Path)

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

	// Recompute and store embedding for updated content
	m.computeAndStoreEmbedding(newContent, page.Path)

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

	// Remove embedding vector
	m.deleteEmbedding(relPath)

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

// Embedder returns the embedding provider, or nil if none is configured.
// This is the single access point for all embedder usage across components.
func (m *Manager) Embedder() llm.EmbeddingProvider {
	return m.embedder
}

// LLM returns the LLM provider, or nil if none is configured.
// This is the single access point for all LLM inference across components.
func (m *Manager) LLM() llm.LLMProvider {
	return m.llmProvider
}

// LLMConfig returns the LLM configuration.
func (m *Manager) LLMConfig() config.LLMConfig {
	return m.llmCfg
}

// Close closes the wiki manager and releases resources (e.g., SQLite connection).
func (m *Manager) Close() error {
	if m.index != nil {
		return m.index.Close()
	}
	return nil
}

// Search searches wiki pages and raw sources.
//
// When an embedder is configured, it uses vector-first hybrid retrieval:
// vector similarity is the primary signal, and FTS5 AND results act as a
// precision booster via RRF. If FTS AND contributes nothing, vector results
// are returned directly (without diluting them via weak FTS OR matches).
//
// When no embedder is configured, it falls back to FTS5 with AND→OR fallback.
//
// This is the primary search entry point for both simple lookups (find command)
// and context retrieval for AI-powered Q&A (ask command).
func (m *Manager) Search(ctx context.Context, query string, topN int) ([]SearchResult, error) {
	m.ensureComponents()

	if m.embedder != nil {
		return m.hybridSearch(ctx, query, topN)
	}
	return m.ftsWithFallback(query, topN)
}

// hybridSearch implements vector-first hybrid retrieval with MMR diversity.
//
// Strategy (vector-first, FTS as pure booster, MMR diversity):
//  1. Vector search retrieves a fixed large recall pool (200) with embedding
//     vectors and similarity scores preserved. The pool size is independent
//     of topN to ensure minority-relevant content (e.g. THP docs) is included.
//  2. FTS5 AND acts as a precision booster via RRF: vector results that also
//     match FTS keywords receive a score bonus. FTS-only results are never
//     introduced as independent candidates.
//  3. FTS5 OR acts as a secondary booster when AND is too strict.
//  4. MMR diversity (λ=0.5, target=50): from the fused pool, iteratively
//     select results that are both relevant to the query AND diverse from
//     already-selected results. The MMR target is fixed at 50 (independent of
//     topN) to give the algorithm enough rounds for the diversity penalty to
//     accumulate — minority-relevant clusters (e.g. THP) get picked after the
//     dominant cluster (e.g. GC) saturates. λ=0.5 balances relevance and
//     diversity equally.
//  5. Truncate to topN: the caller's budget for LLM context. topN only
//     controls how many of the diverse results are returned, not how many
//     MMR selects.
//  6. If vector search fails entirely (embedder down, no vectors indexed),
//     we fall back to the full FTS pipeline (AND → OR).
func (m *Manager) hybridSearch(ctx context.Context, query string, topN int) ([]SearchResult, error) {
	if m.tracer != nil {
		m.tracer.Begin("search", "query", query, "topN", topN, "strategy", "hybrid")
		defer m.tracer.End()
	}

	// Step 1: Vector search with large fixed recall pool (200).
	queryVec, embErr := m.embedder.EmbedQuery(ctx, query)
	if embErr != nil {
		if m.tracer != nil {
			m.tracer.Begin("vector")
			m.tracer.Error(embErr)
			m.tracer.End()
		}
		return m.ftsWithFallback(query, topN)
	}

	const recallSize = 200
	scoredResults, _ := m.index.searchByVectorWithMeta(queryVec, recallSize)
	if len(scoredResults) == 0 {
		if m.tracer != nil {
			m.tracer.Begin("vector")
			m.tracer.End("results", 0)
		}
		return m.ftsWithFallback(query, topN)
	}

	if m.tracer != nil {
		m.tracer.Begin("vector", "pool", recallSize)
		m.tracer.End("results", len(scoredResults), "docs", scoredDocList(scoredResults[:min(5, len(scoredResults))]))
	}

	// Step 2: FTS AND as precision booster via RRF.
	ftsBoosted := false
	if andQuery := toFTS5AndQuery(query); andQuery != "" {
		ftsResults, ftsErr := m.index.searchWithSnippets(andQuery, 5)
		if ftsErr == nil && len(ftsResults) > 0 {
			scoredResults = rrfFuseFull(ftsResults, scoredResults)
			ftsBoosted = true
			if m.tracer != nil {
				m.tracer.Begin("fts", "type", "AND")
				m.tracer.End("results", len(ftsResults), "docs", docList(ftsResults[:min(3, len(ftsResults))]))
			}
		}
	}

	// Step 3: FTS AND returned nothing — try OR.
	if !ftsBoosted {
		if orQuery := toFTS5OrQuery(query); orQuery != "" {
			orResults, err := m.index.searchWithSnippets(orQuery, 5)
			if err == nil && len(orResults) > 0 {
				scoredResults = rrfFuseFull(orResults, scoredResults)
				if m.tracer != nil {
					m.tracer.Begin("fts", "type", "OR")
					m.tracer.End("results", len(orResults), "docs", docList(orResults[:min(3, len(orResults))]))
				}
			}
		}
	}

	if m.tracer != nil {
		m.tracer.Begin("rrf")
		m.tracer.End("fused", len(scoredResults))
	}

	// Step 4: MMR diversity.
	const mmrTarget = 50
	mmrN := mmrTarget
	if mmrN > len(scoredResults) {
		mmrN = len(scoredResults)
	}
	diverse := mmrDiversify(queryVec, scoredResults, 0.5, mmrN)

	if m.tracer != nil {
		m.tracer.Begin("mmr", "lambda", 0.5, "target", mmrTarget)
		m.tracer.End("selected", len(diverse), "docs", docList(diverse[:min(10, len(diverse))]))
	}

	// Step 4.5: LLM rerank.
	if m.llmProvider != nil && len(diverse) > topN {
		beforeRerank := len(diverse)
		diverse = m.rerankWithLLM(ctx, query, diverse)
		if m.tracer != nil {
			m.tracer.Begin("rerank")
			m.tracer.End("candidates", fmt.Sprintf("%d→%d", beforeRerank, len(diverse)),
				"docs", docList(diverse[:min(5, len(diverse))]))
		}
	}

	// Step 5: Truncate to topN.
	if topN < len(diverse) {
		if m.tracer != nil {
			m.tracer.Begin("final")
			m.tracer.End("returned", topN, "docs", docList(diverse[:topN]))
		}
		return diverse[:topN], nil
	}
	if m.tracer != nil {
		m.tracer.Begin("final")
		m.tracer.End("returned", len(diverse), "docs", docList(diverse[:min(5, len(diverse))]))
	}
	return diverse, nil
}

// ftsWithFallback performs FTS5 AND search with CJK bigram expansion,
// falling back to OR if AND returns nothing.
func (m *Manager) ftsWithFallback(query string, topN int) ([]SearchResult, error) {
	if m.tracer != nil {
		m.tracer.Begin("search", "query", query, "topN", topN, "strategy", "fts")
		defer m.tracer.End()
	}

	andQuery := toFTS5AndQuery(query)
	results, err := m.index.searchWithSnippets(andQuery, topN)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		if m.tracer != nil {
			m.tracer.Begin("fts", "type", "AND")
			m.tracer.End("results", 0)
		}
		orQuery := toFTS5OrQuery(query)
		if orQuery != "" {
			results, err = m.index.searchWithSnippets(orQuery, topN)
			if err != nil {
				return nil, err
			}
		}
		if m.tracer != nil {
			m.tracer.Begin("fts", "type", "OR")
			m.tracer.End("results", len(results), "docs", docList(results[:min(5, len(results))]))
		}
	} else {
		if m.tracer != nil {
			m.tracer.Begin("fts", "type", "AND")
			m.tracer.End("results", len(results), "docs", docList(results[:min(5, len(results))]))
		}
	}
	return results, nil
}

// docList formats search result titles for trace output (compact, readable).
func docList(results []SearchResult) string {
	if len(results) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, r := range results {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(r.Title)
		if i >= 9 {
			fmt.Fprintf(&b, ",…(%d total)", len(results))
			break
		}
	}
	b.WriteString("]")
	return b.String()
}
// scoredDocList formats scoredResult titles for trace output.
func scoredDocList(results []scoredResult) string {
	sr := make([]SearchResult, len(results))
	for i, r := range results {
		sr[i] = r.SearchResult
	}
	return docList(sr)
}



// rrfFuse uses Reciprocal Rank Fusion (k=60) to boost vector search results
// with FTS keyword matches. FTS acts as a pure booster: only pages that already
// appear in vector results receive an FTS score bonus. FTS-only candidates are
// never introduced — this prevents keyword matches from displacing semantically
// relevant pages that happen to use different terminology.
func rrfFuse(ftsResults, vecResults []SearchResult, limit int) []SearchResult {
	const k = 60

	// Build set of vector paths — only these can appear in the final output.
	vecPaths := make(map[string]bool, len(vecResults))
	for _, r := range vecResults {
		vecPaths[r.Path] = true
	}

	score := make(map[string]float64)
	result := make(map[string]SearchResult)

	// Vector results always contribute their RRF score.
	for i, r := range vecResults {
		score[r.Path] += 1.0 / (k + float64(i+1))
		if _, exists := result[r.Path]; !exists {
			result[r.Path] = r
		}
	}

	// FTS only boosts pages already found by vector search.
	// FTS-only pages are NOT added as independent candidates.
	for i, r := range ftsResults {
		if vecPaths[r.Path] {
			score[r.Path] += 1.0 / (k + float64(i+1))
			// Keep the vector result metadata (title, snippet may differ).
		}
	}

	// Sort by descending score
	type scored struct {
		SearchResult
		score float64
	}
	var list []scored
	for path, r := range result {
		list = append(list, scored{r, score[path]})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].score > list[j].score
	})

	if limit > len(list) {
		limit = len(list)
	}
	out := make([]SearchResult, limit)
	for i := 0; i < limit; i++ {
		out[i] = list[i].SearchResult
	}
	return out
}

// rrfFuseFull is like rrfFuse but returns the full fused pool (no truncation)
// with score metadata preserved for downstream MMR diversification.
// FTS acts as pure booster: only pages already in vecResults get an FTS bonus.
func rrfFuseFull(ftsResults []SearchResult, vecResults []scoredResult) []scoredResult {
	const k = 60

	// Build set of vector paths and index for ranking.
	vecPaths := make(map[string]bool, len(vecResults))
	for _, r := range vecResults {
		vecPaths[r.Path] = true
	}

	// Compute RRF scores. Start with vector score as base.
	rrfScore := make(map[string]float64)
	for i, r := range vecResults {
		rrfScore[r.Path] = 1.0 / (k + float64(i+1))
	}

	// FTS boosts vector results that also match keywords.
	for i, r := range ftsResults {
		if vecPaths[r.Path] {
			rrfScore[r.Path] += 1.0 / (k + float64(i+1))
		}
	}

	// Build output: each vecResult gets its RRF score. FTS-only entries
	// are excluded (same as rrfFuse semantics).
	out := make([]scoredResult, len(vecResults))
	for i, r := range vecResults {
		out[i] = r
		out[i].score = rrfScore[r.Path] // replace cosine score with RRF score
	}

	// Sort by descending RRF score
	sort.Slice(out, func(i, j int) bool {
		return out[i].score > out[j].score
	})

	return out
}

// computeAndStoreEmbedding computes an embedding for the given content and
// stores it in the vector index. Errors are logged but not returned — embedding
// failures never block page writes.
func (m *Manager) computeAndStoreEmbedding(content, path string) {
	if m.embedder == nil {
		return
	}
	vecs, err := m.embedder.Embed(context.Background(), []string{content})
	if err != nil {
		return
	}
	if len(vecs) == 0 {
		return
	}
	_ = m.index.StoreVector(path, vecs[0])
}

// deleteEmbedding removes the embedding for a page. No-op if no embedder
// is configured or the vector doesn't exist.
func (m *Manager) deleteEmbedding(path string) {
	if m.embedder == nil {
		return
	}
	_ = m.index.DeleteVector(path)
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

// Reindex rebuilds all FTS entries with CJK bigram enrichment. This is needed
// when upgrading from a version that did not have CJK bigram support.
func (m *Manager) Reindex() error {
	m.ensureComponents()

	// Reindex wiki pages
	pages, err := m.List("")
	if err != nil {
		return fmt.Errorf("listing pages: %w", err)
	}
	for _, p := range pages {
		if err := m.index.ReindexContent(p.Path, p.Title, string(p.Type), p.Content); err != nil {
			return fmt.Errorf("reindexing page %s: %w", p.Path, err)
		}
	}

	// Reindex raw sources
	sources, err := m.ListSources("")
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}
	for _, relPath := range sources {
		fullPath := filepath.Join(m.root, relPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("reading raw source %s: %w", relPath, err)
		}
		title := strings.TrimSuffix(filepath.Base(relPath), ".md")
		if err := m.index.ReindexContent(relPath, title, "raw", string(content)); err != nil {
			return fmt.Errorf("reindexing raw source %s: %w", relPath, err)
		}
	}

	return nil
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
