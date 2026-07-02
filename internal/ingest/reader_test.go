package ingest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReader_ReadFile(t *testing.T) {
	t.Run("Markdown", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test-article.md")
		content := "# Hello\n\nThis is a test article."
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		r := NewReader()
		src, err := r.Read(path, "article")
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if src.Title != "test-article" {
			t.Errorf("expected title 'test-article', got %q", src.Title)
		}
		if src.Content != content {
			t.Errorf("expected content %q, got %q", content, src.Content)
		}
		if src.SourceType != "article" {
			t.Errorf("expected sourceType 'article', got %q", src.SourceType)
		}
		if src.Origin != path {
			t.Errorf("expected origin %q, got %q", path, src.Origin)
		}
	})

	t.Run("Text", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "notes.txt")
		content := "Plain text notes."
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		r := NewReader()
		src, err := r.Read(path, "note")
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if src.SourceType != "note" {
			t.Errorf("expected sourceType 'note', got %q", src.SourceType)
		}
	})

	t.Run("UnknownExtension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.log")
		content := "log data"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		r := NewReader()
		src, err := r.Read(path, "note")
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if src.SourceType != "note" {
			t.Errorf("expected sourceType 'note', got %q", src.SourceType)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		r := NewReader()
		_, err := r.Read("/nonexistent/file.md", "note")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

func TestReader_ReadURL(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html><head><title>Test Page</title></head><body>Hello</body></html>"))
		}))
		defer server.Close()

		r := NewReader()
		src, err := r.Read(server.URL, "web")
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if src.Title != "Test Page" {
			t.Errorf("expected title 'Test Page', got %q", src.Title)
		}
		if src.SourceType != "web" {
			t.Errorf("expected sourceType 'web', got %q", src.SourceType)
		}
	})

	t.Run("HTTPError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		r := NewReader()
		_, err := r.Read(server.URL+"/nonexistent", "web")
		if err == nil {
			t.Fatal("expected error for 404")
		}
	})
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		url      string
		content  string
		expected string
	}{
		{"https://example.com/path/article-title", "", "article-title"},
		{"https://example.com/page.html", "", "page"},
		{"https://example.com/page.htm", "", "page"},
		{"https://example.com/a/b?q=1", "", "b"},
		{"https://example.com/a/b#section", "", "b"},
		{"https://example.com/", "", "example.com"},
		{"https://example.com/path/", "", "example.com"},
	}

	for _, tt := range tests {
		got := extractTitle(tt.url, tt.content)
		if got != tt.expected {
			t.Errorf("extractTitle(%q, %q) = %q, want %q", tt.url, tt.content, got, tt.expected)
		}
	}
}
