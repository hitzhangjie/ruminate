package query

import (
	"context"
	"strings"
	"testing"

	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/trace"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

//--------------------------- mock wiki manager ---------------------------//

// stubWiki implements wikiManager for testing.
type stubWiki struct {
	results []wiki.SearchResult
	err     error
	pages   map[string]*wiki.Page
}

func (s *stubWiki) Search(ctx context.Context, query string, topN int, effort wiki.SearchEffort) ([]wiki.SearchResult, error) {
	return s.results, s.err
}
func (s *stubWiki) ReadByPath(path string) (*wiki.Page, error) {
	if s.pages == nil {
		return nil, errNotFound
	}
	p, ok := s.pages[path]
	if !ok {
		return nil, errNotFound
	}
	return p, nil
}
func (s *stubWiki) Read(title string, pageType wiki.PageType) (*wiki.Page, error) {
	return nil, errNotFound
}

// errNotFound simulates a "page not found" error.
var errNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "page not found" }
func (s *stubWiki) Create(title string, t wiki.PageType, content string) (*wiki.Page, error) {
	return &wiki.Page{Title: title, Content: content, Type: t}, nil
}
func (s *stubWiki) Update(title string, t wiki.PageType, content string) (*wiki.Page, error) {
	return &wiki.Page{Title: title, Content: content, Type: t}, nil
}
func (s *stubWiki) Index() *wiki.IndexManager  { return nil }
func (s *stubWiki) SetTracer(tr *trace.Tracer) {}

//-------------------------- mock llm provider ----------------------------//

// stubLLM implements llm.LLMProvider for testing.
type stubLLM struct {
	response string
	err      error
}

func (s *stubLLM) Chat(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &llm.ChatResponse{Content: s.response}, nil
}
func (s *stubLLM) ChatStream(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (<-chan llm.Chunk, error) {
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan llm.Chunk, 1)
	go func() {
		ch <- llm.Chunk{Content: s.response, Done: true}
		close(ch)
	}()
	return ch, nil
}

//---------------------------- the query tests ----------------------------//

func newTestEngine(wm wikiManager, llm llm.LLMProvider) *Engine {
	return &Engine{wiki: wm, llmProvider: llm}
}

func search(title, path, snippet string) wiki.SearchResult {
	return wiki.SearchResult{
		IndexEntry: wiki.IndexEntry{Title: title, Path: path},
		Snippet:    snippet,
	}
}

//---------------------------- engine run ask -----------------------------//

func TestBuildAskMessages(t *testing.T) {
	sw := &stubWiki{pages: map[string]*wiki.Page{
		"wiki/summaries/test.md": {Title: "Test", Content: "content here."},
	}}
	engine := newTestEngine(sw, nil)
	sources := []Source{{Title: "Test", Path: "wiki/summaries/test.md"}}

	msgs := engine.buildAskMessages("question?", sources)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Errorf("roles: %q, %q", msgs[0].Role, msgs[1].Role)
	}
	if !strings.Contains(msgs[0].Content, "Test") || !strings.Contains(msgs[0].Content, "content here") {
		t.Error("system prompt missing content")
	}
	if msgs[1].Content != "question?" {
		t.Errorf("user msg = %q", msgs[1].Content)
	}
}

func TestRetrieveContext(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		sw := &stubWiki{
			results: []wiki.SearchResult{
				search("Page A", "wiki/summaries/a.md", "<b>snippet</b> A"),
				search("Page B", "wiki/summaries/b.md", "<b>snippet</b> B"),
			},
			pages: map[string]*wiki.Page{
				"wiki/summaries/a.md": {Title: "Page A", Content: "cA"},
				"wiki/summaries/b.md": {Title: "Page B", Content: "cB"},
			},
		}
		engine := newTestEngine(sw, nil)

		sources, err := engine.retrieveContext(context.Background(), "q", 10, "fast")
		if err != nil {
			t.Fatalf("retrieveContext() error: %v", err)
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2, got %d", len(sources))
		}
		if strings.Contains(sources[0].Snippet, "<b>") {
			t.Error("snippet should have <b> tags stripped")
		}
	})

	t.Run("SkipsMissing", func(t *testing.T) {
		sw := &stubWiki{
			results: []wiki.SearchResult{
				search("Exists", "wiki/summaries/exists.md", "e"),
				search("Missing", "wiki/summaries/missing.md", "m"),
			},
			pages: map[string]*wiki.Page{
				"wiki/summaries/exists.md": {Title: "Exists", Content: "c"},
			},
		}
		engine := newTestEngine(sw, nil)

		sources, err := engine.retrieveContext(context.Background(), "q", 10, "fast")
		if err != nil {
			t.Fatalf("retrieveContext() error: %v", err)
		}
		if len(sources) != 1 || sources[0].Title != "Exists" {
			t.Errorf("got %d sources, first=%q, want 1 source 'Exists'", len(sources), sources[0].Title)
		}
	})
}

func TestEngine_Ask(t *testing.T) {
	t.Run("NoProvider", func(t *testing.T) {
		sw := &stubWiki{
			results: []wiki.SearchResult{
				search("Test", "wiki/summaries/test.md", "snippet"),
			},
			pages: map[string]*wiki.Page{
				"wiki/summaries/test.md": {Title: "Test", Content: "content"},
			}}
		engine := newTestEngine(sw, nil)
		_, err := engine.Ask(context.Background(), "q", &AskOptions{})
		if err == nil || !strings.Contains(err.Error(), "no LLM provider") {
			t.Errorf("expected 'no LLM provider' error, got: %v", err)
		}
	})

	t.Run("NoResults", func(t *testing.T) {
		engine := newTestEngine(&stubWiki{}, &stubLLM{response: "unused"})
		result, err := engine.Ask(context.Background(), "q", &AskOptions{})
		if err != nil {
			t.Fatalf("Ask() error: %v", err)
		}
		if !strings.Contains(result.Answer, "couldn't find") {
			t.Errorf("got %q, want 'couldn't find' message", result.Answer)
		}
	})

	t.Run("Success", func(t *testing.T) {
		sw := &stubWiki{
			results: []wiki.SearchResult{
				search("Test Page", "wiki/summaries/test.md", "<b>test</b>"),
			},
			pages: map[string]*wiki.Page{
				"wiki/summaries/test.md": {Title: "Test Page", Content: "Test content."},
			},
		}
		sl := &stubLLM{response: "The answer."}
		engine := newTestEngine(sw, sl)

		result, err := engine.Ask(context.Background(), "q", &AskOptions{})
		if err != nil {
			t.Fatalf("Ask() error: %v", err)
		}
		if result.Answer != "The answer." {
			t.Errorf("Answer = %q, want 'The answer.'", result.Answer)
		}
		if len(result.Sources) != 1 || result.Sources[0].Title != "Test Page" {
			t.Errorf("Sources = %+v", result.Sources)
		}
	})

	t.Run("Save", func(t *testing.T) {
		sw := &stubWiki{
			results: []wiki.SearchResult{search("P", "wiki/summaries/p.md", "s")},
			pages:   map[string]*wiki.Page{"wiki/summaries/p.md": {Title: "P", Content: "c"}},
		}
		engine := newTestEngine(sw, &stubLLM{response: "ok"})
		result, err := engine.Ask(context.Background(), "q", &AskOptions{Save: true})
		if err != nil {
			t.Fatalf("Ask() with save error: %v", err)
		}
		if result.Answer != "ok" {
			t.Errorf("Answer = %q, want 'ok'", result.Answer)
		}
	})
}

//-------------------------------- others ---------------------------------//

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "1234567890", 10, "1234567890"},
		{"truncate", "hello world", 8, "hello..."},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.s, tt.maxLen); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestStripTags(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"no tags", "hello", "hello"},
		{"bold", "<b>hi</b>", "hi"},
		{"only tags", "<b></b>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripTags(tt.input); got != tt.want {
				t.Errorf("stripTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSourceDocList(t *testing.T) {
	if got := sourceDocList(nil); got != "[]" {
		t.Errorf("nil = %q, want []", got)
	}
	if got := sourceDocList([]Source{{Title: "A"}, {Title: "B"}}); got != "[A,B]" {
		t.Errorf("two = %q, want [A,B]", got)
	}
}

