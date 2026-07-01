package wiki

import (
	"regexp"
	"strings"
)

// wikiLinkPattern matches [[page-name]] or [[display text|page-name]].
// Also matches [[page-name#section]] for anchor links.
var wikiLinkPattern = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// ParseWikiLinks extracts all WikiLink targets from Markdown content.
// Supports these formats:
//   - [[page-name]]        → target is "page-name"
//   - [[display|page-name]] → target is "page-name"
//   - [[page-name#section]] → target is "page-name"
func ParseWikiLinks(content string) []string {
	matches := wikiLinkPattern.FindAllStringSubmatch(content, -1)

	seen := make(map[string]bool)
	var links []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		inner := match[1]

		// Handle [[display text|page-name]] format
		if idx := strings.Index(inner, "|"); idx >= 0 {
			inner = inner[idx+1:]
		}

		// Handle [[page-name#section]] format
		if idx := strings.Index(inner, "#"); idx >= 0 {
			inner = inner[:idx]
		}

		inner = strings.TrimSpace(inner)
		if inner == "" {
			continue
		}

		if !seen[inner] {
			seen[inner] = true
			links = append(links, inner)
		}
	}

	return links
}

// ResolveWikiLink resolves a WikiLink target to a file path within the wiki.
// The resolution strategy:
//  1. If a colon is present (e.g., "entities:karpathy"), use type:name format
//  2. Otherwise, search across all page types
func ResolveWikiLink(target string, pageTypes []PageType) (title string, pageType PageType) {
	// Check for type-prefixed format: "entities:karpathy"
	if idx := strings.Index(target, ":"); idx >= 0 {
		typeStr := target[:idx]
		title = target[idx+1:]
		for _, pt := range pageTypes {
			if string(pt) == typeStr {
				pageType = pt
				return
			}
		}
	}
	// No type prefix: use the target as-is, page type must be resolved externally
	return target, ""
}

// GenerateWikiLink creates a WikiLink string for a given page title.
func GenerateWikiLink(title string) string {
	return "[[" + title + "]]"
}
