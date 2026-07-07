package cmd

import (
	"testing"

	"github.com/hitzhangjie/ruminate/internal/lint"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// =============================================================================
// parseEffort (ask.go)
// =============================================================================

func TestParseEffort(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  wiki.SearchEffort
	}{
		{"fast", "fast", wiki.SearchEffortFast},
		{"balanced", "balanced", wiki.SearchEffortBalanced},
		{"thorough", "thorough", wiki.SearchEffortThorough},
		{"unknown defaults to fast", "nonexistent", wiki.SearchEffortFast},
		{"empty defaults to fast", "", wiki.SearchEffortFast},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEffort(tt.input)
			if got != tt.want {
				t.Errorf("parseEffort(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEffort_Constants(t *testing.T) {
	// Verify that the effort constants have expected values.
	if wiki.SearchEffortFast != "fast" {
		t.Errorf("SearchEffortFast = %q, want 'fast'", wiki.SearchEffortFast)
	}
	if wiki.SearchEffortBalanced != "balanced" {
		t.Errorf("SearchEffortBalanced = %q, want 'balanced'", wiki.SearchEffortBalanced)
	}
	if wiki.SearchEffortThorough != "thorough" {
		t.Errorf("SearchEffortThorough = %q, want 'thorough'", wiki.SearchEffortThorough)
	}
}

// =============================================================================
// ansiHighlight / plainHighlight (find.go)
// =============================================================================

func TestAnsiHighlight(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"bold tags", "<b>hello</b> world", "\033[1mhello\033[0m world"},
		{"multiple bold", "<b>a</b> <b>b</b>", "\033[1ma\033[0m \033[1mb\033[0m"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansiHighlight(tt.input)
			if got != tt.want {
				t.Errorf("ansiHighlight(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAnsiHighlight_EdgeCases(t *testing.T) {
	t.Run("only open tag", func(t *testing.T) {
		got := ansiHighlight("<b>text")
		want := "\033[1mtext"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("only close tag", func(t *testing.T) {
		got := ansiHighlight("text</b>")
		want := "text\033[0m"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestPlainHighlight(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"bold tags stripped", "<b>hello</b> world", "hello world"},
		{"multiple bold stripped", "<b>a</b> <b>b</b>", "a b"},
		{"empty string", "", ""},
		{"only bold tags", "<b></b>", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plainHighlight(tt.input)
			if got != tt.want {
				t.Errorf("plainHighlight(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPlainHighlight_NoTags(t *testing.T) {
	input := "just plain text without any tags at all"
	got := plainHighlight(input)
	if got != input {
		t.Errorf("plainHighlight should preserve tag-free text: got %q, want %q", got, input)
	}
}

// =============================================================================
// severityIcon / checkLabel / wrapLines (lint.go)
// =============================================================================

func TestSeverityIcon(t *testing.T) {
	tests := []struct {
		name     string
		severity lint.Severity
		want     string
	}{
		{"error", lint.SeverityError, "✗"},
		{"warning", lint.SeverityWarning, "⚠"},
		{"info", lint.SeverityInfo, "ℹ"},
		{"unknown", lint.Severity("unknown"), "•"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := severityIcon(tt.severity)
			if got != tt.want {
				t.Errorf("severityIcon(%q) = %q, want %q", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSeverityIcon_AllDefined(t *testing.T) {
	// Ensure all defined severities map to non-empty icons.
	sevs := []lint.Severity{lint.SeverityError, lint.SeverityWarning, lint.SeverityInfo}
	for _, s := range sevs {
		icon := severityIcon(s)
		if icon == "" {
			t.Errorf("severityIcon(%q) returned empty string", s)
		}
	}
}

func TestCheckLabel(t *testing.T) {
	tests := []struct {
		name  string
		check string
		want  string
	}{
		{"orphan", lint.CheckOrphan, "Orphaned & Unreferenced Pages"},
		{"broken_link", lint.CheckBrokenLink, "Broken Links"},
		{"staleness", lint.CheckStaleness, "Stale Content"},
		{"contradiction", lint.CheckContradiction, "Potential Contradictions"},
		{"unknown falls back to check name", "custom_check", "custom_check"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkLabel(tt.check)
			if got != tt.want {
				t.Errorf("checkLabel(%q) = %q, want %q", tt.check, got, tt.want)
			}
		})
	}
}

func TestCheckLabel_EdgeCases(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		got := checkLabel("")
		if got != "" {
			t.Errorf("checkLabel('') = %q, want ''", got)
		}
	})
}

func TestWrapLines(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{
			name:  "short text no wrapping",
			text:  "hello world",
			width: 20,
			want:  []string{"hello world"},
		},
		{
			name:  "empty text",
			text:  "",
			width: 20,
			want:  []string{""},
		},
		{
			name:  "wrap at word boundary",
			text:  "hello world foo bar baz qux",
			width: 15,
			want:  []string{"hello world foo", "bar baz qux"},
		},
		{
			name:  "multi-line wrapping",
			text:  "the quick brown fox jumps over the lazy dog",
			width: 14,
			want:  []string{"the quick", "brown fox", "jumps over the", "lazy dog"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapLines(tt.text, tt.width)
			if len(got) != len(tt.want) {
				t.Errorf("wrapLines(%q, %d) = %v (len=%d), want %v (len=%d)", tt.text, tt.width, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("wrapLines line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWrapLines_EdgeCases(t *testing.T) {
	t.Run("single word longer than width", func(t *testing.T) {
		got := wrapLines("supercalifragilisticexpialidocious", 10)
		if len(got) != 1 {
			t.Errorf("expected 1 line, got %d: %v", len(got), got)
		}
	})

	t.Run("exact width match", func(t *testing.T) {
		got := wrapLines("1234567890", 10)
		if len(got) != 1 || got[0] != "1234567890" {
			t.Errorf("got %v, want [1234567890]", got)
		}
	})

	t.Run("multiple spaces", func(t *testing.T) {
		got := wrapLines("a  b  c  d  e  f", 6)
		if len(got) < 2 {
			t.Errorf("should wrap: %v", got)
		}
	})
}

func TestWrapLines_Multibyte(t *testing.T) {
	t.Run("Chinese characters", func(t *testing.T) {
		// wrapLines breaking on word boundaries
		got := wrapLines("你好世界测试文本", 6)
		// Each CJK character counts as one rune.
		if len(got) < 1 {
			t.Errorf("expected at least 1 line, got %v", got)
		}
	})
}
