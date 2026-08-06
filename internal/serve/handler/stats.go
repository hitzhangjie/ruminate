package handler

import (
	"net/http"
	"time"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// StatsResponse is GET /api/stats?wiki=.
type StatsResponse struct {
	Wiki      string                `json:"wiki"`
	Summaries int                   `json:"summaries"`
	Entities  int                   `json:"entities"`
	Concepts  int                   `json:"concepts"`
	Synthesis int                   `json:"synthesis"`
	Pages     int                   `json:"pages"`
	Sources   int                   `json:"sources"`
	Links     int                   `json:"links"`
	Topics    []wiki.Topic          `json:"topics,omitempty"`
	Recent    []wiki.RecentActivity `json:"recent,omitempty"`
	Updated   string                `json:"updated"` // RFC3339
}

// Stats handles GET /api/stats — knowledge-base scale for the home dashboard.
// Pass ?wiki= to select a knowledge base in multi-wiki mode.
func (a *API) Stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	eng, name, err := a.resolveEngine(r, "")
	if err != nil {
		writeWikiError(w, err)
		return
	}

	st, err := eng.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated := st.Updated
	if updated.IsZero() {
		updated = time.Now().UTC()
	}

	writeJSON(w, http.StatusOK, StatsResponse{
		Wiki:      name,
		Summaries: st.Summaries,
		Entities:  st.Entities,
		Concepts:  st.Concepts,
		Synthesis: st.Synthesis,
		Pages:     st.Pages,
		Sources:   st.Sources,
		Links:     st.Links,
		Topics:    st.Topics,
		Recent:    st.Recent,
		Updated:   updated.Format(time.RFC3339),
	})
}
