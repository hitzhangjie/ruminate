package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/agent"
	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// QueryService is the ask + dashboard surface used by HTTP handlers.
// *query.Engine implements this interface.
type QueryService interface {
	Ask(ctx context.Context, question string, opts *query.AskOptions) (*query.AskResult, error)
	AskStream(ctx context.Context, question string, opts *query.AskOptions) (<-chan query.AskChunk, error)
	AskAgent(ctx context.Context, question string, opts *agent.Options) (*agent.Result, error)
	Stats() (*wiki.WikiStats, error)
}

// WikiHub resolves a wiki name to a QueryService.
// Implemented by serve.Hub.
type WikiHub interface {
	// Resolve returns the engine for name (empty → default).
	Resolve(name string) (QueryService, string /*resolved*/, error)
	// Catalog lists available wiki names.
	Catalog() []string
	// Default is the default wiki name.
	Default() string
	// Fixed is non-empty when the server is locked to one wiki (--wiki).
	Fixed() string
	// Multi is true when the UI may offer a wiki selector.
	Multi() bool
}

// API groups HTTP handlers that share a multi-wiki hub.
type API struct {
	hub WikiHub
}

// NewAPI constructs handlers bound to a wiki hub.
func NewAPI(hub WikiHub) *API {
	return &API{hub: hub}
}

// Register mounts API routes on mux.
//
//	GET  /api/health
//	GET  /api/wikis
//	GET  /api/stats?wiki=
//	POST /api/ask
//	POST /api/ask/stream
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", a.Health)
	mux.HandleFunc("GET /api/wikis", a.ListWikis)
	mux.HandleFunc("GET /api/stats", a.Stats)
	mux.HandleFunc("POST /api/ask", a.Ask)
	mux.HandleFunc("POST /api/ask/stream", a.AskStream)
}

// resolveEngine picks the wiki for this request.
// Precedence: explicit name → X-Wiki header → ?wiki= → hub default.
func (a *API) resolveEngine(r *http.Request, explicit string) (QueryService, string, error) {
	name := strings.TrimSpace(explicit)
	if name == "" {
		name = strings.TrimSpace(r.Header.Get("X-Wiki"))
	}
	if name == "" {
		name = strings.TrimSpace(r.URL.Query().Get("wiki"))
	}
	eng, resolved, err := a.hub.Resolve(name)
	if err != nil {
		return nil, "", err
	}
	return eng, resolved, nil
}

func writeWikiError(w http.ResponseWriter, err error) {
	msg := err.Error()
	status := http.StatusInternalServerError
	if strings.Contains(msg, "unknown wiki") ||
		strings.Contains(msg, "locked to wiki") ||
		strings.Contains(msg, "not in catalog") {
		status = http.StatusBadRequest
	}
	writeError(w, status, msg)
}
