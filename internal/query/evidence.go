package query

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// EvidenceMode controls L1→L2 layered retrieval (docs/108).
type EvidenceMode string

const (
	// EvidenceWiki uses only Synthesis (wiki) — default, backward compatible.
	EvidenceWiki EvidenceMode = "wiki"
	// EvidenceAuto escalates to raw when wiki context looks insufficient.
	EvidenceAuto EvidenceMode = "auto"
	// EvidenceRaw always attaches contributing raw sources for hit wiki pages.
	EvidenceRaw EvidenceMode = "raw"
)

// ParseEvidenceMode converts a CLI string to EvidenceMode.
func ParseEvidenceMode(s string) EvidenceMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return EvidenceAuto
	case "raw":
		return EvidenceRaw
	default:
		return EvidenceWiki
	}
}

// evidenceTriggerWords hint that the user wants original text / precise facts.
var evidenceTriggerWords = []string{
	"原文", "原话", "怎么说", "接口签名", "默认值", "源码", "源代码",
	"source code", "original", "verbatim", "exact quote", "signature",
	"default value", "raw text", "according to the source",
}

// needsEvidenceEscalation decides whether L1 context alone is insufficient.
func needsEvidenceEscalation(question string, sources []Source) bool {
	if len(sources) < 2 {
		return true
	}
	q := strings.ToLower(question)
	for _, w := range evidenceTriggerWords {
		if strings.Contains(q, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// rawLayerReader is the subset of wiki access needed for L2 escalation.
type rawLayerReader interface {
	ReadByPath(path string) (*wiki.Page, error)
	SearchRaw(query string, topN int) ([]wiki.SearchResult, error)
}

// attachEvidence loads contributing raw sources for wiki hits and appends
// them as additional context Sources (Layer=evidence).
//
// maxRawChars caps total raw characters injected (shared budget).
func attachEvidence(reader rawLayerReader, wikiSources []Source, question string, maxRawChars int) []Source {
	if maxRawChars <= 0 {
		maxRawChars = 48 * 1024
	}

	out := make([]Source, 0, len(wikiSources)+4)
	for _, s := range wikiSources {
		s.Layer = "wiki"
		out = append(out, s)
	}

	seen := make(map[string]bool)
	budget := maxRawChars

	// Prefer frontmatter sources on hit wiki pages.
	for _, s := range wikiSources {
		page, err := reader.ReadByPath(s.Path)
		if err != nil {
			continue
		}
		for _, ref := range page.Sources {
			if ref.Path == "" || seen[ref.Path] {
				continue
			}
			seen[ref.Path] = true
			rawSrc, n := loadRawSource(reader, ref.Path, budget)
			if rawSrc == nil {
				continue
			}
			budget -= n
			out = append(out, *rawSrc)
			if budget <= 0 {
				return out
			}
		}
	}

	// If still no raw attached, try raw_fts search as a secondary path.
	if budget == maxRawChars {
		if sr, ok := reader.(interface {
			SearchRaw(string, int) ([]wiki.SearchResult, error)
		}); ok {
			results, err := sr.SearchRaw(question, 3)
			if err == nil {
				for _, r := range results {
					if seen[r.Path] {
						continue
					}
					seen[r.Path] = true
					rawSrc, n := loadRawSource(reader, r.Path, budget)
					if rawSrc == nil {
						continue
					}
					budget -= n
					out = append(out, *rawSrc)
					if budget <= 0 {
						break
					}
				}
			}
		}
	}

	return out
}

func loadRawSource(reader rawLayerReader, path string, budget int) (*Source, int) {
	page, err := reader.ReadByPath(path)
	if err != nil {
		return nil, 0
	}
	body := page.Content
	// Prefer body without huge size blow-up
	if utf8.RuneCountInString(body) > budget && budget > 0 {
		runes := []rune(body)
		if budget > 64 {
			body = string(runes[:budget]) + "\n…[truncated]"
		} else {
			body = string(runes[:budget])
		}
	}
	snippet := body
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	return &Source{
		Title:   fmt.Sprintf("[raw] %s", page.Title),
		Path:    path,
		Snippet: snippet,
		Layer:   "raw",
		Content: body,
	}, utf8.RuneCountInString(body)
}
