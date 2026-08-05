package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hitzhangjie/ruminate/internal/agent"
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
// resolveAskMode (ask.go) — --mode=agent|rag + flag scope
// =============================================================================

func TestResolveAskMode_DefaultsToAgent(t *testing.T) {
	resetAskFlags(t)
	mode, err := resolveAskMode(askCmd)
	if err != nil {
		t.Fatalf("resolveAskMode: %v", err)
	}
	if mode != askModeAgent {
		t.Errorf("default mode = %q, want %q", mode, askModeAgent)
	}
}

func TestResolveAskMode_RAG(t *testing.T) {
	resetAskFlags(t)
	askMode = askModeRAG
	// RAG-only flags may be set when mode is rag.
	setFlagChanged(askCmd, "effort", true)
	setFlagChanged(askCmd, "evidence", true)
	mode, err := resolveAskMode(askCmd)
	if err != nil {
		t.Fatalf("resolveAskMode: %v", err)
	}
	if mode != askModeRAG {
		t.Errorf("mode = %q, want %q", mode, askModeRAG)
	}
}

func TestResolveAskMode_RAGFlagsRejectOnAgent(t *testing.T) {
	resetAskFlags(t)
	askMode = askModeAgent
	setFlagChanged(askCmd, "effort", true)
	_, err := resolveAskMode(askCmd)
	if err == nil {
		t.Fatal("expected error for --effort under agent mode")
	}
	if !strings.Contains(err.Error(), "--effort") || !strings.Contains(err.Error(), "rag") {
		t.Errorf("error should mention --effort and rag: %v", err)
	}
}

func TestResolveAskMode_EvidenceRejectOnAgent(t *testing.T) {
	resetAskFlags(t)
	askMode = askModeAgent
	setFlagChanged(askCmd, "evidence", true)
	_, err := resolveAskMode(askCmd)
	if err == nil {
		t.Fatal("expected error for --evidence under agent mode")
	}
}

func TestResolveAskMode_AgentFlagsRejectOnRAG(t *testing.T) {
	resetAskFlags(t)
	askMode = askModeRAG
	setFlagChanged(askCmd, "max-steps", true)
	_, err := resolveAskMode(askCmd)
	if err == nil {
		t.Fatal("expected error for --max-steps under rag mode")
	}
	if !strings.Contains(err.Error(), "--max-steps") || !strings.Contains(err.Error(), "agent") {
		t.Errorf("error should mention --max-steps and agent: %v", err)
	}
}

func TestResolveAskMode_InvalidMode(t *testing.T) {
	resetAskFlags(t)
	askMode = "pipeline"
	_, err := resolveAskMode(askCmd)
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
}

// resetAskFlags restores package-level ask flag vars and clears Flag.Changed.
func resetAskFlags(t *testing.T) {
	t.Helper()
	askNoStream = false
	askTopN = 20
	askEffort = "fast"
	askEvidence = "auto"
	askMode = askModeAgent
	askMaxSteps = agent.DefaultMaxSteps
	askAgentRoot = nil
	askCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
	})
}

func setFlagChanged(cmd *cobra.Command, name string, changed bool) {
	f := cmd.Flags().Lookup(name)
	if f != nil {
		f.Changed = changed
	}
}

// =============================================================================
// writeAgentStep (ask.go) — ReAct observability output (docs/111 Phase A)
// =============================================================================

func TestWriteAgentStep_CompactToolTurn(t *testing.T) {
	var buf strings.Builder
	s := agent.Step{
		Index:            1,
		Thought:          "search the wiki for GC",
		Action:           "wiki_search",
		Args:             map[string]any{"query": "GC"},
		Observation:      "found GC page with concurrent collector notes",
		ObsBytes:         48,
		Duration:         120 * time.Millisecond,
		PromptTokens:     100,
		CompletionTokens: 20,
	}
	writeAgentStep(&buf, s, false)
	out := buf.String()
	// One-line timeline: no multi-line Thought/Observation dump.
	for _, want := range []string{
		"✓ 1",
		"wiki_search",
		`"GC"`,
		"120ms",
		"48B",
		"100→20 tok",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in compact output:\n%s", want, out)
		}
	}
	for _, ban := range []string{
		"Thought:",
		"Observation",
		"Decide prompt:",
		"── Step",
	} {
		if strings.Contains(out, ban) {
			t.Errorf("compact output should not contain %q:\n%s", ban, out)
		}
	}
	// Single line (plus trailing newline).
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Errorf("expected single line compact output, got:\n%s", out)
	}
}

func TestWriteAgentStep_VerboseIncludesPrompts(t *testing.T) {
	var buf strings.Builder
	s := agent.Step{
		Index:        1,
		Thought:      "look around",
		Action:       "wiki_index",
		Args:         map[string]any{},
		Observation:  "index.md …",
		ObsBytes:     10,
		Duration:     50 * time.Millisecond,
		SystemPrompt: "You are Ruminate's exploration agent.",
		UserPrompt:   "Question: What is GC?\n\nDecide the next action…",
		LLMRaw:       `{"thought":"look around","action":"wiki_index","args":{}}`,
	}
	writeAgentStep(&buf, s, true)
	out := buf.String()
	for _, want := range []string{
		"── Step 1 · tool",
		"Decide prompt:",
		"[system]",
		"You are Ruminate's exploration agent.",
		"[user]",
		"Question: What is GC?",
		"LLM response",
		`"action":"wiki_index"`,
		"Thought:",
		"Action: wiki_index",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in verbose output:\n%s", want, out)
		}
	}
}

func TestWriteAgentStep_FinalAnswerCompact(t *testing.T) {
	var buf strings.Builder
	s := agent.Step{
		Index:       3,
		Thought:     "enough evidence",
		Final:       true,
		FinalAnswer: "Go has a concurrent GC.",
		Duration:    80 * time.Millisecond,
	}
	writeAgentStep(&buf, s, false)
	out := buf.String()
	for _, want := range []string{
		"→ 3",
		"final_answer",
		"80ms",
		"enough evidence",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// Full answer body stays on stdout after the run; compact must not dump it.
	if strings.Contains(out, "Go has a concurrent GC.") {
		t.Errorf("compact final step should not print full answer:\n%s", out)
	}
	if strings.Contains(out, "Thought:") {
		t.Errorf("compact final should not use multi-line Thought block:\n%s", out)
	}
}

func TestWriteAgentStep_FinalAnswerVerbose(t *testing.T) {
	var buf strings.Builder
	s := agent.Step{
		Index:       3,
		Thought:     "enough evidence",
		Final:       true,
		FinalAnswer: "Go has a concurrent GC.",
		Duration:    80 * time.Millisecond,
	}
	writeAgentStep(&buf, s, true)
	out := buf.String()
	for _, want := range []string{
		"Step 3 · final_answer",
		"Thought:",
		"enough evidence",
		"Final answer",
		"Go has a concurrent GC.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestWriteAgentStep_ParseErrorExpands(t *testing.T) {
	var buf strings.Builder
	s := agent.Step{
		Index:         2,
		Thought:       "parse_error",
		Observation:   "ERROR: could not parse decision JSON: unexpected EOF",
		ObsBytes:      50,
		ParseDumpPath: "/tmp/wiki/db/debug/parse_errors/x.txt",
		Duration:      10 * time.Millisecond,
	}
	writeAgentStep(&buf, s, false)
	out := buf.String()
	for _, want := range []string{
		"── Step 2 · parse_error",
		"unparseable JSON",
		"dump: /tmp/wiki/db/debug/parse_errors/x.txt",
		"Observation · ERROR",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestWriteAgentStep_ToolErrorExpands(t *testing.T) {
	var buf strings.Builder
	s := agent.Step{
		Index:       4,
		Thought:     "read the file",
		Action:      "file_read",
		Args:        map[string]any{"path": "missing.go"},
		Observation: "ERROR: path outside sandbox roots",
		ObsBytes:    34,
		Duration:    5 * time.Millisecond,
	}
	writeAgentStep(&buf, s, false)
	out := buf.String()
	for _, want := range []string{
		"── Step 4 · tool",
		"Action: file_read",
		"summary: missing.go",
		"Observation · ERROR",
		"path outside sandbox",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// Non-verbose error expand still should not dump decide prompts.
	if strings.Contains(out, "Decide prompt:") {
		t.Error("error expand without -v should not print Decide prompt")
	}
}

func TestTruncateOneLine(t *testing.T) {
	if got := truncateOneLine("  a   b\nc  ", 100); got != "a b c" {
		t.Errorf("collapse whitespace: got %q", got)
	}
	if got := truncateOneLine("hello world", 8); got != "hello w…" {
		t.Errorf("truncate: got %q", got)
	}
	// Long paths keep the basename (suffix), not the prefix.
	path := "/Users/zhangjie/hitzhangjie/project/internal/runtime/proc.go"
	got := truncateOneLine(path, 24)
	if !strings.HasSuffix(got, "proc.go") {
		t.Errorf("path should keep basename, got %q", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("path should lead with ellipsis, got %q", got)
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
