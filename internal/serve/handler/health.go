package handler

import (
	"net/http"
)

// HealthResponse is GET /api/health.
type HealthResponse struct {
	Status  string   `json:"status"`
	Wiki    string   `json:"wiki,omitempty"`     // resolved default/requested wiki
	Wikis   []string `json:"wikis,omitempty"`    // catalog
	Default string   `json:"default,omitempty"`
	Fixed   string   `json:"fixed,omitempty"`
	Multi   bool     `json:"multi"`
}

// Health handles GET /api/health.
// Optional ?wiki= validates that the named wiki can be opened.
func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Touch the default (or requested) engine so misconfiguration surfaces early.
	_, resolved, err := a.resolveEngine(r, "")
	if err != nil {
		writeWikiError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, HealthResponse{
		Status:  "ok",
		Wiki:    resolved,
		Wikis:   a.hub.Catalog(),
		Default: a.hub.Default(),
		Fixed:   a.hub.Fixed(),
		Multi:   a.hub.Multi(),
	})
}
