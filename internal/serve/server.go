// Package serve provides the HTTP API + embedded web UI server for Ruminate.
package serve

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/hitzhangjie/ruminate/internal/serve/handler"
	"github.com/hitzhangjie/ruminate/internal/serve/middleware"
	"github.com/hitzhangjie/ruminate/internal/serve/static"
)

// Config configures the HTTP server.
type Config struct {
	Host string
	Port int
	Hub  *Hub
}

// Server is the Ruminate HTTP server (API + web UI on one port).
type Server struct {
	cfg   Config
	http  *http.Server
	addr  string
	hasUI bool
}

// New creates a Server that serves health + ask APIs and the embedded web UI.
func New(cfg Config) *Server {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port <= 0 {
		cfg.Port = 8420
	}
	if cfg.Hub == nil {
		panic("serve.New: Hub is required")
	}

	mux := http.NewServeMux()
	api := handler.NewAPI(cfg.Hub)
	api.Register(mux)

	hasUI := false
	if uiFS, err := static.FS(); err == nil {
		hasUI = true
		// Catch-all for the SPA. Method-specific /api/* routes take precedence
		// in Go 1.22+ ServeMux over this generic pattern.
		mux.Handle("/", spaHandler(uiFS))
	} else {
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = fmt.Fprintf(w, `{"service":"ruminate","error":"web UI not embedded","health":"/api/health"}`+"\n")
		})
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return &Server{
		cfg:   cfg,
		addr:  addr,
		hasUI: hasUI,
		http: &http.Server{
			Addr:              addr,
			Handler:           middleware.CORS(mux),
			ReadHeaderTimeout: 10 * time.Second,
			// Agent runs can take minutes; keep write deadline generous.
			// Per-request cancellation still applies via context.
			WriteTimeout: 0,
			IdleTimeout:  120 * time.Second,
		},
	}
}

// Addr returns the listen address (host:port).
func (s *Server) Addr() string {
	return s.addr
}

// ListenAndServe starts the server and blocks until it shuts down.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}

	base := fmt.Sprintf("http://%s", s.addr)
	mode := "multi-wiki"
	if s.cfg.Hub.Fixed() != "" {
		mode = "wiki=" + s.cfg.Hub.Fixed()
	} else if !s.cfg.Hub.Multi() {
		mode = "wiki=" + s.cfg.Hub.Default()
	}
	log.Printf("ruminate serve: listening on %s (%s)", base, mode)
	if s.hasUI {
		if static.Available() {
			log.Printf("  UI   %s/  (and /chat)", base)
		} else {
			log.Printf("  UI   %s/  (placeholder — run `make build`)", base)
		}
	}
	log.Printf("  API  GET  %s/api/health", base)
	log.Printf("  API  GET  %s/api/wikis", base)
	log.Printf("  API  GET  %s/api/stats?wiki=", base)
	log.Printf("  API  POST %s/api/ask", base)
	log.Printf("  API  POST %s/api/ask/stream", base)
	log.Printf("  wikis: %v (default=%s)", s.cfg.Hub.Catalog(), s.cfg.Hub.Default())
	return s.http.Serve(ln)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
