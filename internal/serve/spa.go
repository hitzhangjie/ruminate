package serve

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves a single-page app from fsys.
// Existing files are returned as-is; unknown paths fall back to index.html
// so client-side routes (/chat, /wiki/…) work under BrowserRouter.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Never let the SPA swallow API traffic (defence in depth if routing changes).
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		upath := path.Clean("/" + r.URL.Path)
		rel := strings.TrimPrefix(upath, "/")
		if rel == "" || rel == "." {
			rel = "index.html"
		}

		// If the requested file is missing, serve index.html for SPA routes.
		if _, err := fs.Stat(fsys, rel); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			// Serve index.html explicitly so FileServer does not 404 directories.
			serveIndex(w, r, fsys)
			return
		}

		// Prevent directory listing: directories without index fall back to SPA shell.
		if info, err := fs.Stat(fsys, rel); err == nil && info.IsDir() {
			if _, err := fs.Stat(fsys, path.Join(rel, "index.html")); err != nil {
				serveIndex(w, r, fsys)
				return
			}
		}

		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "UI not available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Avoid stale SPA shell after rebuilds when browser caches index aggressively.
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}
