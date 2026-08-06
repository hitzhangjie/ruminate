package handler

import (
	"net/http"
)

// WikiInfo is one registered knowledge base.
type WikiInfo struct {
	Name string `json:"name"`
}

// WikisResponse is GET /api/wikis.
type WikisResponse struct {
	Wikis   []WikiInfo `json:"wikis"`
	Default string     `json:"default"`
	// Fixed is set when the server was started with --wiki (selector disabled).
	Fixed string `json:"fixed,omitempty"`
	// Multi is true when the client may switch wikis.
	Multi bool `json:"multi"`
}

// ListWikis handles GET /api/wikis.
func (a *API) ListWikis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	names := a.hub.Catalog()
	list := make([]WikiInfo, 0, len(names))
	for _, n := range names {
		list = append(list, WikiInfo{Name: n})
	}
	writeJSON(w, http.StatusOK, WikisResponse{
		Wikis:   list,
		Default: a.hub.Default(),
		Fixed:   a.hub.Fixed(),
		Multi:   a.hub.Multi(),
	})
}
