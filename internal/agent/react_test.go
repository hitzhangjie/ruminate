package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hitzhangjie/ruminate/internal/agent/tools"
	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

func TestParseDecision(t *testing.T) {
	raw := `{"thought":"search wiki","action":"wiki_search","args":{"query":"GC"}}`
	d, err := parseDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "wiki_search" || d.Args["query"] != "GC" {
		t.Fatalf("unexpected: %+v", d)
	}

	fenced := "```json\n{\"final_answer\":\"hello\",\"citations\":[]}\n```"
	d2, err := parseDecision(fenced)
	if err != nil {
		t.Fatal(err)
	}
	if d2.FinalAnswer != "hello" {
		t.Fatalf("final=%q", d2.FinalAnswer)
	}
}

func TestSandboxResolve(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "wiki")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "a.md")
	if err := os.WriteFile(f, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	sb, err := tools.NewSandbox([]string{dir}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := sb.Resolve("wiki/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if abs != f {
		// May differ by symlink resolution
		if filepath.Clean(abs) != filepath.Clean(f) {
			t.Errorf("got %s want %s", abs, f)
		}
	}
	// Outside root
	if _, err := sb.Resolve("/etc/passwd"); err == nil {
		t.Error("expected error for path outside roots")
	}
}

func TestCodeToolsEnclosing(t *testing.T) {
	dir := t.TempDir()
	src := `package sample

func Hello() string {
	return "hi"
}

func World() {}
`
	path := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	sb, err := tools.NewSandbox([]string{dir}, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	tools.RegisterCodeTools(reg, sb)

	obs, err := reg.Exec(context.Background(), "read_enclosing", map[string]any{
		"path": "sample.go",
		"line": 4,
	}, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(obs, "func Hello") {
		t.Fatalf("expected Hello function body, got:\n%s", obs)
	}

	obs2, err := reg.Exec(context.Background(), "symbol_search", map[string]any{
		"name": "Hello",
	}, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(obs2, "Hello") {
		t.Fatalf("expected Hello candidate: %s", obs2)
	}
}

// scriptedLLM returns canned responses in order.
type scriptedLLM struct {
	responses []string
	i         int
}

func (s *scriptedLLM) Chat(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
	if s.i >= len(s.responses) {
		return &llm.ChatResponse{Content: `{"final_answer":"fallback","citations":[]}`}, nil
	}
	r := s.responses[s.i]
	s.i++
	return &llm.ChatResponse{Content: r}, nil
}

func (s *scriptedLLM) ChatStream(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (<-chan llm.Chunk, error) {
	return nil, nil
}

func TestExplorerRun(t *testing.T) {
	dir := t.TempDir()
	mgr := wiki.NewManager(dir, nil, nil)
	if err := mgr.Init(); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	// Seed a wiki page
	content := wiki.WithSources("# GC\n\nGo has a concurrent garbage collector.\n", "GC", wiki.PageTypeConcept, nil)
	if _, err := mgr.Create("GC", wiki.PageTypeConcept, content); err != nil {
		t.Fatal(err)
	}

	searchArgs, _ := json.Marshal(map[string]any{"query": "GC"})
	_ = searchArgs
	llmStub := &scriptedLLM{responses: []string{
		`{"thought":"search","action":"wiki_search","args":{"query":"GC"}}`,
		`{"thought":"done","final_answer":"Go has a concurrent GC.","citations":[{"title":"GC","path":"wiki/concepts/GC.md","layer":"wiki"}]}`,
	}}

	ex := NewExplorer(mgr, llmStub, config.LLMConfig{Temperature: 0.1})
	result, err := ex.Run(context.Background(), "What about GC?", &Options{
		MaxSteps: 5,
		WallTime: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Answer, "concurrent GC") {
		t.Errorf("answer=%q", result.Answer)
	}
	if len(result.Steps) < 2 {
		t.Errorf("expected >=2 steps, got %d", len(result.Steps))
	}
}
