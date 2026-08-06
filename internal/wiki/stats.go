package wiki

import (
	"fmt"
	"sort"
	"time"
)

// WikiStats is a snapshot of knowledge-base scale for the home dashboard.
// It answers: how large is this mind, and what kinds of things live in it?
type WikiStats struct {
	// Counts by page type (wiki synthesis layer).
	Summaries int `json:"summaries"`
	Entities  int `json:"entities"`
	Concepts  int `json:"concepts"`
	Synthesis int `json:"synthesis"`
	// Pages is Summaries+Entities+Concepts+Synthesis.
	Pages int `json:"pages"`
	// Sources is the number of raw Evidence files (ingest inputs).
	Sources int `json:"sources"`
	// Links is the number of unique WikiLink targets found across pages.
	Links int `json:"links"`
	// Topics is a sample of entity/concept titles for the constellation view.
	Topics []Topic `json:"topics,omitempty"`
	// Recent is the latest write operations from log.md.
	Recent []RecentActivity `json:"recent,omitempty"`
	// Updated is when this snapshot was taken (server clock).
	Updated time.Time `json:"updated"`
}

// Topic is a lightweight page label for UI chips.
type Topic struct {
	Title string   `json:"title"`
	Type  PageType `json:"type"`
}

// RecentActivity is one log.md line for the "recently woven" feed.
type RecentActivity struct {
	Date      string   `json:"date"` // YYYY-MM-DD
	Operation string   `json:"operation"`
	PageType  PageType `json:"page_type"`
	Title     string   `json:"title"`
}

const (
	maxTopics = 36
	maxRecent = 10
)

// Stats computes a dashboard snapshot of the wiki.
// Safe for personal-scale wikis (hundreds of pages); walks wiki/ once.
func (m *Manager) Stats() (*WikiStats, error) {
	m.ensureComponents()

	pages, err := m.List("")
	if err != nil {
		return nil, fmt.Errorf("listing pages: %w", err)
	}

	stats := &WikiStats{
		Updated: time.Now().UTC(),
	}

	linkSet := make(map[string]struct{})
	var concepts, entities []Topic

	for _, p := range pages {
		switch p.Type {
		case PageTypeSummary:
			stats.Summaries++
		case PageTypeEntity:
			stats.Entities++
			entities = append(entities, Topic{Title: p.Title, Type: p.Type})
		case PageTypeConcept:
			stats.Concepts++
			concepts = append(concepts, Topic{Title: p.Title, Type: p.Type})
		case PageTypeSynthesis:
			stats.Synthesis++
		}
		for _, target := range ParseWikiLinks(p.Content) {
			linkSet[target] = struct{}{}
		}
	}

	stats.Pages = stats.Summaries + stats.Entities + stats.Concepts + stats.Synthesis
	stats.Links = len(linkSet)

	sources, err := m.ListSources("")
	if err != nil {
		return nil, fmt.Errorf("listing sources: %w", err)
	}
	stats.Sources = len(sources)

	stats.Topics = sampleTopics(concepts, entities, maxTopics)

	if entries, err := m.Log().RecentEntries(maxRecent); err == nil {
		stats.Recent = make([]RecentActivity, 0, len(entries))
		for _, e := range entries {
			date := ""
			if !e.Date.IsZero() {
				date = e.Date.Format("2006-01-02")
			}
			stats.Recent = append(stats.Recent, RecentActivity{
				Date:      date,
				Operation: e.Operation,
				PageType:  e.PageType,
				Title:     e.Title,
			})
		}
	}

	return stats, nil
}

// sampleTopics interleaves concepts and entities so the constellation feels mixed.
func sampleTopics(concepts, entities []Topic, limit int) []Topic {
	// Stable sort by title for deterministic UI (not random flicker).
	sort.Slice(concepts, func(i, j int) bool { return concepts[i].Title < concepts[j].Title })
	sort.Slice(entities, func(i, j int) bool { return entities[i].Title < entities[j].Title })

	out := make([]Topic, 0, limit)
	i, j := 0, 0
	// Prefer concepts slightly (they're thematic), but keep entities for density.
	for len(out) < limit && (i < len(concepts) || j < len(entities)) {
		if i < len(concepts) {
			out = append(out, concepts[i])
			i++
		}
		if len(out) >= limit {
			break
		}
		if j < len(entities) {
			out = append(out, entities[j])
			j++
		}
	}
	return out
}
