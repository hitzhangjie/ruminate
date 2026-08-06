package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hitzhangjie/ruminate/internal/agent"
	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// mockQuery implements QueryService for handler tests.
type mockQuery struct {
	name         string
	askResult    *query.AskResult
	askErr       error
	streamChunks []query.AskChunk
	streamErr    error
	agentResult  *agent.Result
	agentErr     error
	stats        *wiki.WikiStats
	statsErr     error
}

func (m *mockQuery) Stats() (*wiki.WikiStats, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	if m.stats != nil {
		return m.stats, nil
	}
	return &wiki.WikiStats{}, nil
}

func (m *mockQuery) Ask(ctx context.Context, question string, opts *query.AskOptions) (*query.AskResult, error) {
	return m.askResult, m.askErr
}

func (m *mockQuery) AskStream(ctx context.Context, question string, opts *query.AskOptions) (<-chan query.AskChunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan query.AskChunk, len(m.streamChunks))
	for _, c := range m.streamChunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockQuery) AskAgent(ctx context.Context, question string, opts *agent.Options) (*agent.Result, error) {
	if opts != nil {
		if opts.OnProgress != nil {
			opts.OnProgress(agent.Progress{Phase: agent.ProgressDecide, Step: 1})
		}
		if opts.OnStep != nil && m.agentResult != nil {
			for _, s := range m.agentResult.Steps {
				opts.OnStep(s)
			}
		}
	}
	return m.agentResult, m.agentErr
}

// mockHub implements WikiHub.
type mockHub struct {
	engines map[string]QueryService
	def     string
	fixed   string
}

func newMockHub(def string, engines map[string]QueryService) *mockHub {
	return &mockHub{engines: engines, def: def}
}

func (h *mockHub) Catalog() []string {
	names := make([]string, 0, len(h.engines))
	for n := range h.engines {
		names = append(names, n)
	}
	return names
}
func (h *mockHub) Default() string { return h.def }
func (h *mockHub) Fixed() string   { return h.fixed }
func (h *mockHub) Multi() bool {
	return h.fixed == "" && len(h.engines) > 1
}
func (h *mockHub) Resolve(name string) (QueryService, string, error) {
	if name == "" {
		name = h.def
	}
	if h.fixed != "" && name != h.fixed {
		return nil, "", fmt.Errorf("server is locked to wiki %q (started with --wiki)", h.fixed)
	}
	eng, ok := h.engines[name]
	if !ok {
		return nil, "", fmt.Errorf("unknown wiki %q", name)
	}
	return eng, name, nil
}

func TestHealth(t *testing.T) {
	mq := &mockQuery{name: "demo"}
	api := NewAPI(newMockHub("demo", map[string]QueryService{"demo": mq}))
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Wiki != "demo" {
		t.Fatalf("body = %+v", body)
	}
}

func TestStats(t *testing.T) {
	mq := &mockQuery{
		stats: &wiki.WikiStats{
			Summaries: 3,
			Entities:  24,
			Concepts:  14,
			Synthesis: 2,
			Pages:     43,
			Sources:   3,
			Links:     50,
			Topics:    []wiki.Topic{{Title: "Go", Type: wiki.PageTypeEntity}},
		},
	}
	api := NewAPI(newMockHub("demo", map[string]QueryService{"demo": mq}))
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body StatsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Wiki != "demo" || body.Pages != 43 || body.Entities != 24 {
		t.Fatalf("body = %+v", body)
	}
}

func TestStats_SelectWiki(t *testing.T) {
	a := &mockQuery{stats: &wiki.WikiStats{Pages: 1, Entities: 1}}
	b := &mockQuery{stats: &wiki.WikiStats{Pages: 9, Entities: 9}}
	api := NewAPI(newMockHub("a", map[string]QueryService{"a": a, "b": b}))
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/stats?wiki=b", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body StatsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Wiki != "b" || body.Pages != 9 {
		t.Fatalf("body=%+v", body)
	}
}

func TestListWikis(t *testing.T) {
	api := NewAPI(newMockHub("a", map[string]QueryService{
		"a": &mockQuery{},
		"b": &mockQuery{},
	}))
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/wikis", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var body WikisResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if !body.Multi || body.Default != "a" || len(body.Wikis) != 2 {
		t.Fatalf("body=%+v", body)
	}
}

func TestAsk_MissingQuestion(t *testing.T) {
	api := NewAPI(newMockHub("demo", map[string]QueryService{"demo": &mockQuery{}}))
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
}

func TestAsk_Agent(t *testing.T) {
	mq := &mockQuery{
		agentResult: &agent.Result{
			Answer: "hello from agent",
			Refs:   []wiki.Ref{{Title: "Page", Path: "concepts/page.md", Layer: "wiki"}},
			Steps: []agent.Step{{
				Index: 1, Action: "wiki_search", Duration: 50 * time.Millisecond, Final: false,
			}},
		},
	}
	api := NewAPI(newMockHub("demo", map[string]QueryService{"demo": mq}))
	mux := http.NewServeMux()
	api.Register(mux)

	body := `{"question":"what is x?"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp AskResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Answer != "hello from agent" || resp.Mode != "agent" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAsk_RAG(t *testing.T) {
	mq := &mockQuery{
		askResult: &query.AskResult{
			Answer: "rag answer",
			Refs:   []wiki.Ref{{Title: "R", Path: "r.md"}},
		},
	}
	api := NewAPI(newMockHub("demo", map[string]QueryService{"demo": mq}))
	mux := http.NewServeMux()
	api.Register(mux)

	body := `{"question":"q","mode":"rag"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp AskResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Answer != "rag answer" || resp.Mode != "rag" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestAskStream_AgentSSE(t *testing.T) {
	mq := &mockQuery{
		agentResult: &agent.Result{
			Answer: "final",
			Refs:   []wiki.Ref{{Title: "T", Path: "t.md"}},
			Steps: []agent.Step{{
				Index: 1, Action: "wiki_index", Duration: 10 * time.Millisecond,
			}},
		},
	}
	api := NewAPI(newMockHub("demo", map[string]QueryService{"demo": mq}))
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/ask/stream",
		bytes.NewBufferString(`{"question":"stream me"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	events := parseSSE(t, rr.Body.String())
	var types []string
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	if !contains(types, "progress") || !contains(types, "step") || !contains(types, "done") {
		t.Fatalf("event types = %v", types)
	}
}

func TestAskStream_RAG(t *testing.T) {
	mq := &mockQuery{
		streamChunks: []query.AskChunk{
			{Content: "Hel"},
			{Content: "lo"},
			{Done: true, Refs: []wiki.Ref{{Title: "X", Path: "x.md"}}},
		},
	}
	api := NewAPI(newMockHub("demo", map[string]QueryService{"demo": mq}))
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/ask/stream",
		bytes.NewBufferString(`{"question":"q","mode":"rag"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	events := parseSSE(t, rr.Body.String())
	var content strings.Builder
	var done *StreamEvent
	for i := range events {
		switch events[i].Type {
		case "content":
			content.WriteString(events[i].Content)
		case "done":
			done = &events[i]
		}
	}
	if content.String() != "Hello" {
		t.Fatalf("content = %q", content.String())
	}
	if done == nil || done.Answer != "Hello" || done.Mode != "rag" {
		t.Fatalf("done = %+v", done)
	}
}

func TestNormalizeMode(t *testing.T) {
	if normalizeMode("") != modeAgent {
		t.Fatal("default should be agent")
	}
	if normalizeMode("RAG") != modeRAG {
		t.Fatal("want rag")
	}
}

func parseSSE(t *testing.T, body string) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var ev StreamEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatalf("parse SSE %q: %v", payload, err)
		}
		events = append(events, ev)
	}
	if len(events) == 0 {
		t.Fatalf("no SSE events in body:\n%s", body)
	}
	return events
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
