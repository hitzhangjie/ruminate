// Package static embeds the built web UI for ruminate serve.
//
// The Vite app is built into dist/ (see web/vite.config.ts outDir).
// A minimal placeholder keeps `go build` working before the first npm build.
package static

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distEmbed embed.FS

// FS returns the UI filesystem rooted at the dist contents (index.html at root).
func FS() (fs.FS, error) {
	return fs.Sub(distEmbed, "dist")
}

// Available reports whether a real UI bundle is present (not only the placeholder).
// Placeholder is detected by the absence of hashed assets under assets/.
func Available() bool {
	sub, err := FS()
	if err != nil {
		return false
	}
	// Real Vite builds always produce assets/; the placeholder does not.
	entries, err := fs.ReadDir(sub, "assets")
	if err != nil {
		return false
	}
	return len(entries) > 0
}
