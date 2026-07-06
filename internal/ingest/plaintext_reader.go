package ingest

import "os"

// PlainTextReader handles plain-text file formats that can be read directly
// via os.ReadFile. It supports common text-based knowledge formats.
type PlainTextReader struct{}

// Extensions returns the supported plain-text file extensions.
func (r *PlainTextReader) Extensions() []string {
	return []string{".md", ".txt", ".markdown", ".org", ".rst", ".log", ".text"}
}

// Read reads the file and returns its raw content.
func (r *PlainTextReader) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}
