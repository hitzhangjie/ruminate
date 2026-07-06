package ingest

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

// Source represents a read source material ready for analysis.
type Source struct {
	// Title is a human-readable identifier for the source.
	Title string

	// Content is the full text content.
	Content string

	// SourceType is a user-defined label: "article", "paper", "note", "book".
	SourceType string

	// Origin is the original path or URL.
	Origin string
}

// Reader reads source materials from files or URLs.
//
// For file reading, Reader maintains a registry of FileReader implementations
// keyed by lowercase file extension. When Read is called on a file, the Reader
// dispatches to the matching FileReader based on the file's extension.
//
// Built-in readers (PlainTextReader) are registered automatically. Additional
// readers can be registered via Register() to support new file formats.
type Reader struct {
	client  *http.Client
	readers map[string]FileReader // extension -> reader, lowercase keys
}

// NewReader creates a new Reader with built-in file readers registered.
func NewReader() *Reader {
	r := &Reader{
		client:  &http.Client{},
		readers: make(map[string]FileReader),
	}
	r.Register(&PlainTextReader{})
	return r
}

// Register adds a FileReader to the dispatch table.
// It maps each of the reader's Extensions() (lowercased) to this reader.
// If an extension is already registered, it is overwritten — this allows
// custom readers to replace built-in ones for specific formats.
func (r *Reader) Register(fr FileReader) {
	for _, ext := range fr.Extensions() {
		r.readers[strings.ToLower(ext)] = fr
	}
}

// SupportedExtensions returns the set of all registered extensions, sorted.
// Each extension includes the leading dot and is lowercase.
func (r *Reader) SupportedExtensions() []string {
	exts := make([]string, 0, len(r.readers))
	for ext := range r.readers {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

// IsSupportedExtension reports whether the given extension has a registered reader.
// The extension should include the leading dot (e.g. ".md").
// Matching is case-insensitive.
func (r *Reader) IsSupportedExtension(ext string) bool {
	_, ok := r.readers[strings.ToLower(ext)]
	return ok
}

// Read reads a source from a file path or URL.
// Files are detected by non-URL paths (no scheme).
// For files, dispatches to the registered FileReader for the file's extension.
// Supported formats are determined by registered FileReaders (PlainTextReader
// handles .md, .txt, etc. by default).
// sourceType is a user-defined label: "article", "paper", "note", "book".
func (r *Reader) Read(pathOrURL, sourceType string) (*Source, error) {
	if isURL(pathOrURL) {
		return r.readURL(pathOrURL, sourceType)
	}
	return r.readFile(pathOrURL, sourceType)
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func (r *Reader) readFile(path, sourceType string) (*Source, error) {
	ext := strings.ToLower(filepath.Ext(path))
	fr, ok := r.readers[ext]
	if !ok {
		return nil, &ErrUnsupportedFileType{Path: path, Ext: ext}
	}

	content, err := fr.Read(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	return &Source{
		Title:      title,
		Content:    string(content),
		SourceType: sourceType,
		Origin:     path,
	}, nil
}

func (r *Reader) readURL(url, sourceType string) (*Source, error) {
	resp, err := r.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching URL %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	content := string(body)
	title := extractTitle(url, content)

	return &Source{
		Title:      title,
		Content:    content,
		SourceType: sourceType,
		Origin:     url,
	}, nil
}

// extractTitle derives a title from URL path or HTML content.
func extractTitle(url, content string) string {
	// Try to extract from HTML title tag
	if strings.HasPrefix(strings.TrimSpace(content), "<") {
		if start := strings.Index(content, "<title>"); start != -1 {
			start += len("<title>")
			if end := strings.Index(content[start:], "</title>"); end != -1 {
				title := strings.TrimSpace(content[start : start+end])
				if title != "" {
					return title
				}
			}
		}
	}

	// Try to extract from URL path
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if last != "" {
			// Remove query/hash
			last = strings.SplitN(last, "?", 2)[0]
			last = strings.SplitN(last, "#", 2)[0]
			// Remove extension
			last = strings.TrimSuffix(last, ".html")
			last = strings.TrimSuffix(last, ".htm")
			if last != "" {
				return last
			}
		}
	}
	// Fallback: derive from domain
	if len(parts) >= 3 {
		return parts[2] // hostname
	}
	return "web-source"
}
