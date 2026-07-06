package ingest

import "fmt"

// FileReader knows how to read a specific file format and extract raw content.
//
// Each FileReader implementation declares which extensions it handles,
// and the Reader dispatches to it based on the file's extension.
//
// To add support for a new file format (e.g. docx, xlsx):
//
//  1. Create a type that implements FileReader
//  2. Call Reader.Register(yourReader) before calling Reader.Read
//
// Built-in readers (PlainTextReader) are registered automatically by NewReader().
type FileReader interface {
	// Extensions returns the file extensions this reader handles.
	// Each extension should include the leading dot and be lowercase,
	// e.g. []string{".md", ".txt"}.
	Extensions() []string

	// Read reads the file at the given path and returns its raw content.
	// The path is guaranteed to have one of the extensions returned by
	// Extensions() (matched case-insensitively).
	Read(path string) ([]byte, error)
}

// ErrUnsupportedFileType is returned when no registered FileReader handles
// the given file extension.
type ErrUnsupportedFileType struct {
	Path string
	Ext  string
}

func (e *ErrUnsupportedFileType) Error() string {
	return fmt.Sprintf("unsupported file type %q: %s", e.Ext, e.Path)
}
