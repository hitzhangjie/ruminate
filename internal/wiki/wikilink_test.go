package wiki

import (
	"reflect"
	"testing"
)

func TestParseWikiLinks(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "no links",
			content:  "This is plain text without any links.",
			expected: nil,
		},
		{
			name:     "single simple link",
			content:  "See [[karpathy]] for details.",
			expected: []string{"karpathy"},
		},
		{
			name:     "multiple links",
			content:  "[[page1]] and [[page2]] and [[page3]]",
			expected: []string{"page1", "page2", "page3"},
		},
		{
			name:     "link with display text",
			content:  "See [[Andrej Karpathy|karpathy]] for details.",
			expected: []string{"karpathy"},
		},
		{
			name:     "link with section anchor",
			content:  "See [[karpathy#early-life]] for his background.",
			expected: []string{"karpathy"},
		},
		{
			name:     "link with display text and anchor",
			content:  "See [[his early life|karpathy#early-life]] for background.",
			expected: []string{"karpathy"},
		},
		{
			name:     "duplicate links deduplicated",
			content:  "[[same]] and [[same]] again.",
			expected: []string{"same"},
		},
		{
			name:     "empty content",
			content:  "",
			expected: nil,
		},
		{
			name:     "edge case: brackets without valid link",
			content:  "Not a [[ valid link.",
			expected: nil,
		},
		{
			name:     "link with spaces",
			content:  "See [[large language model]] for more.",
			expected: []string{"large language model"},
		},
		{
			name:     "pipe with empty target",
			content:  "See [[display|]] here.",
			expected: nil,
		},
		{
			name:     "mixed format links",
			content:  "[[simple]], [[display|piped]], [[anchor#section]], [[full|target#section]]",
			expected: []string{"simple", "piped", "anchor", "target"},
		},
		{
			name:     "link with newlines (regex matches across lines)",
			content:  "[[page\nname]]",
			expected: []string{"page\nname"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWikiLinks(tt.content)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ParseWikiLinks(%q) = %v, want %v", tt.content, got, tt.expected)
			}
		})
	}
}

func TestGenerateWikiLink(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"karpathy", "[[karpathy]]"},
		{"large language model", "[[large language model]]"},
		{"", "[[]]"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := GenerateWikiLink(tt.title)
			if got != tt.expected {
				t.Errorf("GenerateWikiLink(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestResolveWikiLink(t *testing.T) {
	types := []PageType{PageTypeSummary, PageTypeEntity, PageTypeConcept, PageTypeSynthesis}

	tests := []struct {
		name         string
		target       string
		expectedTitle string
		expectedType  PageType
	}{
		{
			name:          "type-prefixed link",
			target:        "entities:karpathy",
			expectedTitle: "karpathy",
			expectedType:  PageTypeEntity,
		},
		{
			name:          "type-prefixed concept",
			target:        "concepts:rag-vs-wiki",
			expectedTitle: "rag-vs-wiki",
			expectedType:  PageTypeConcept,
		},
		{
			name:          "simple link without type prefix",
			target:        "karpathy",
			expectedTitle: "karpathy",
			expectedType:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, pt := ResolveWikiLink(tt.target, types)
			if title != tt.expectedTitle {
				t.Errorf("title = %q, want %q", title, tt.expectedTitle)
			}
			if pt != tt.expectedType {
				t.Errorf("pageType = %q, want %q", pt, tt.expectedType)
			}
		})
	}
}
