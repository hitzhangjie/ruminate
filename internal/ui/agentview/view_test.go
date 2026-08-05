package agentview

import (
	"strings"
	"testing"
	"time"

	"github.com/hitzhangjie/ruminate/internal/agent"
)

func TestView_OnStepSuccessCard(t *testing.T) {
	var buf strings.Builder
	off := false
	v := New(Config{Writer: &buf, Color: &off})
	v.OnStep(agent.Step{
		Index:            1,
		Thought:          "search wiki for history",
		Action:           "wiki_search",
		Args:             map[string]any{"query": "distributed systems history"},
		Observation:      "found three pages…",
		ObsBytes:         2200,
		Duration:         2 * time.Second,
		PromptTokens:     100,
		CompletionTokens: 20,
	})
	out := buf.String()
	for _, want := range []string{
		"wiki_search",
		"distributed systems history",
		"2s",
		"2.1kB",
		"100→20 tok",
		"⎿",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Success cards should not dump full thought by default.
	if strings.Contains(out, "search wiki for history") {
		t.Errorf("success card should not show full thought:\n%s", out)
	}
}

func TestView_OnStepErrorShowsMessage(t *testing.T) {
	var buf strings.Builder
	off := false
	v := New(Config{Writer: &buf, Color: &off})
	v.OnStep(agent.Step{
		Index:       4,
		Thought:     "try Chinese query",
		Action:      "wiki_search",
		Args:        map[string]any{"query": "分布式 系统 历史"},
		Observation: "ERROR: searching FTS with snippets: SQL logic error: fts5: syntax error near \"\"系统\"\"",
		ObsBytes:    91,
		Duration:    45 * time.Second,
	})
	out := buf.String()
	for _, want := range []string{
		"✗",
		"wiki_search",
		"分布式",
		"fts5: syntax error",
		"try Chinese query", // short thought preview on errors
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestView_OnStepFinal(t *testing.T) {
	var buf strings.Builder
	off := false
	v := New(Config{Writer: &buf, Color: &off})
	v.OnStep(agent.Step{
		Index:       5,
		Thought:     "enough evidence",
		Final:       true,
		FinalAnswer: "Key events include ARPANET…",
		Duration:    200 * time.Millisecond,
	})
	out := buf.String()
	if !strings.Contains(out, "final_answer") {
		t.Errorf("expected final_answer label:\n%s", out)
	}
	if strings.Contains(out, "ARPANET") {
		t.Errorf("card should not print full final answer body:\n%s", out)
	}
}

func TestView_OnProgressNonTTY(t *testing.T) {
	var buf strings.Builder
	off := false
	v := New(Config{Writer: &buf, Color: &off})
	v.OnProgress(agent.Progress{Phase: agent.ProgressDecide, Step: 2})
	v.OnProgress(agent.Progress{
		Phase:  agent.ProgressTool,
		Step:   2,
		Action: "wiki_index",
		Args:   map[string]any{"filter": "Distributed"},
	})
	out := buf.String()
	if !strings.Contains(out, "Thinking") {
		t.Errorf("expected Thinking progress:\n%s", out)
	}
	if !strings.Contains(out, "wiki_index") {
		t.Errorf("expected tool progress:\n%s", out)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("你好世界", 3); got != "你好…" {
		t.Errorf("got %q", got)
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Errorf("got %q", got)
	}
}

func TestShortenDetail_PathKeepsBasename(t *testing.T) {
	long := "/Users/zhangjie/hitzhangjie/some-very-long-project/internal/runtime/proc.go"
	got := shortenDetail(long, 40)
	if !strings.HasSuffix(got, "proc.go") {
		t.Errorf("expected basename preserved, got %q", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("expected leading ellipsis, got %q", got)
	}
	if runeLen(got) > 40 {
		t.Errorf("len=%d want <=40: %q", runeLen(got), got)
	}
}

func TestShortenDetail_QueryKeepsPrefix(t *testing.T) {
	q := `"distributed systems history events and more"`
	got := shortenDetail(q, 20)
	if !strings.HasPrefix(got, `"distributed`) {
		t.Errorf("query should keep prefix, got %q", got)
	}
}

func TestCardTitle_LongPathShowsOnSecondary(t *testing.T) {
	var buf strings.Builder
	off := false
	v := New(Config{Writer: &buf, Color: &off})
	long := "/Users/zhangjie/hitzhangjie/ruminate-workspace/internal/runtime/proc.go"
	v.OnStep(agent.Step{
		Index:    1,
		Action:   "file_read",
		Args:     map[string]any{"path": long},
		ObsBytes: 100,
		Duration: time.Second,
	})
	out := buf.String()
	// Basename should appear somewhere (primary suffix and/or secondary full path).
	if !strings.Contains(out, "proc.go") {
		t.Errorf("basename missing from card:\n%s", out)
	}
	if !strings.Contains(out, "file_read") {
		t.Errorf("tool name missing:\n%s", out)
	}
}

func TestLooksLikePath(t *testing.T) {
	if !looksLikePath("/tmp/foo.go") {
		t.Error("abs path")
	}
	if !looksLikePath("pkg/runtime/proc.go:88") {
		t.Error("path:line")
	}
	if looksLikePath(`"调度 系统"`) {
		t.Error("quoted query is not a path")
	}
}
