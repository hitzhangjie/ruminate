package ingest

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
type Reader struct {
	client *http.Client
}

// NewReader creates a new Reader.
func NewReader() *Reader {
	return &Reader{client: &http.Client{}}
}

// Read reads a source from a file path or URL.
// Files are detected by non-URL paths (no scheme).
// Supported file formats: .md, .txt.
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
	content, err := os.ReadFile(path)
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
