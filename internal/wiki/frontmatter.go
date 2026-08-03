package wiki

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SourceRef is a contributing Evidence source for a Synthesis page.
// See docs/108-dual-truth-and-layered-retrieval.md.
type SourceRef struct {
	Path       string `yaml:"path" json:"path"`
	IngestedAt string `yaml:"ingested_at,omitempty" json:"ingested_at,omitempty"`
}

// FrontMatter is the YAML front matter block on wiki pages.
type FrontMatter struct {
	Title   string      `yaml:"title,omitempty"`
	Type    string      `yaml:"type,omitempty"`
	Sources []SourceRef `yaml:"sources,omitempty"`
}

// ParseFrontMatter splits Markdown content into front matter and body.
// If no front matter is present, fm is empty and body is the full content.
func ParseFrontMatter(content string) (fm FrontMatter, body string, err error) {
	content = strings.TrimPrefix(content, "\uFEFF") // strip BOM
	if !strings.HasPrefix(content, "---") {
		return FrontMatter{}, content, nil
	}

	// Find the closing --- on its own line.
	rest := content[3:]
	// Allow optional newline after opening ---
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	} else {
		// "---" not followed by newline — treat as no front matter
		return FrontMatter{}, content, nil
	}

	// Locate closing fence
	var closeIdx int
	var closeLen int
	if i := strings.Index(rest, "\n---\n"); i >= 0 {
		closeIdx = i
		closeLen = len("\n---\n")
	} else if i := strings.Index(rest, "\n---\r\n"); i >= 0 {
		closeIdx = i
		closeLen = len("\n---\r\n")
	} else if i := strings.Index(rest, "\n---"); i >= 0 && i+4 == len(rest) {
		// ends with \n---
		closeIdx = i
		closeLen = len("\n---")
	} else {
		// Unclosed front matter — treat whole content as body
		return FrontMatter{}, content, nil
	}

	yamlBlock := rest[:closeIdx]
	body = rest[closeIdx+closeLen:]

	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return FrontMatter{}, content, fmt.Errorf("parsing front matter: %w", err)
	}
	return fm, body, nil
}

// FormatFrontMatter renders a FrontMatter as a YAML block followed by body.
func FormatFrontMatter(fm FrontMatter, body string) string {
	// Always emit a clean front matter when sources or metadata exist.
	data, err := yaml.Marshal(&fm)
	if err != nil {
		// Fall back to body only on marshal failure (should never happen).
		return body
	}
	// yaml.Marshal appends a trailing newline.
	yamlStr := strings.TrimSpace(string(data))
	body = strings.TrimPrefix(body, "\n")
	return "---\n" + yamlStr + "\n---\n\n" + body
}

// SetFrontMatter replaces or injects front matter while preserving the body.
func SetFrontMatter(content string, fm FrontMatter) string {
	_, body, err := ParseFrontMatter(content)
	if err != nil {
		// On parse error keep original content and prepend
		return FormatFrontMatter(fm, content)
	}
	return FormatFrontMatter(fm, body)
}

// ExtractSources returns contributing sources from page content front matter.
func ExtractSources(content string) []SourceRef {
	fm, _, err := ParseFrontMatter(content)
	if err != nil {
		return nil
	}
	return fm.Sources
}

// MergeSourceRefs appends refs that are not already present (by path).
// Existing entries keep their ingested_at; new ones are added at the end.
func MergeSourceRefs(existing []SourceRef, add ...SourceRef) []SourceRef {
	seen := make(map[string]int, len(existing))
	out := make([]SourceRef, 0, len(existing)+len(add))
	for _, s := range existing {
		if s.Path == "" {
			continue
		}
		if _, ok := seen[s.Path]; ok {
			continue
		}
		seen[s.Path] = len(out)
		out = append(out, s)
	}
	for _, s := range add {
		if s.Path == "" {
			continue
		}
		if _, ok := seen[s.Path]; ok {
			continue
		}
		seen[s.Path] = len(out)
		out = append(out, s)
	}
	return out
}

// NewSourceRef builds a SourceRef with today's date (UTC).
func NewSourceRef(path string) SourceRef {
	return SourceRef{
		Path:       path,
		IngestedAt: time.Now().UTC().Format("2006-01-02"),
	}
}

// WithSources ensures content has front matter containing the given sources.
// Title and page type are written into front matter for discoverability.
func WithSources(content, title string, pageType PageType, sources []SourceRef) string {
	fm, body, err := ParseFrontMatter(content)
	if err != nil {
		body = content
		fm = FrontMatter{}
	}
	if title != "" {
		fm.Title = title
	}
	if pageType != "" {
		fm.Type = string(pageType)
	}
	fm.Sources = MergeSourceRefs(fm.Sources, sources...)
	return FormatFrontMatter(fm, body)
}

// AddSourceToContent merges one source into the page's front matter.
func AddSourceToContent(content, title string, pageType PageType, ref SourceRef) string {
	return WithSources(content, title, pageType, []SourceRef{ref})
}

// PageTypeLabel maps PageType constants to short front-matter type labels.
func PageTypeLabel(pt PageType) string {
	switch pt {
	case PageTypeSummary:
		return "summary"
	case PageTypeEntity:
		return "entity"
	case PageTypeConcept:
		return "concept"
	case PageTypeSynthesis:
		return "synthesis"
	default:
		return string(pt)
	}
}
