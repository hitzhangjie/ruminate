package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hitzhangjie/ruminate/internal/agent"
	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

const (
	modeAgent = "agent"
	modeRAG   = "rag"
)

// Ask handles POST /api/ask (non-streaming JSON).
func (a *API) Ask(w http.ResponseWriter, r *http.Request) {
	req, err := parseAskRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	eng, _, err := a.resolveEngine(r, req.Wiki)
	if err != nil {
		writeWikiError(w, err)
		return
	}

	mode := normalizeMode(req.Mode)
	ctx := r.Context()

	switch mode {
	case modeAgent:
		opts := agentOptions(req)
		result, err := eng.AskAgent(ctx, req.Question, opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, AskResponse{
			Answer:    result.Answer,
			Refs:      refsToDTO(result.Refs),
			Steps:     stepsToDTO(result.Steps),
			Truncated: result.Truncated,
			Mode:      modeAgent,
		})

	case modeRAG:
		opts := ragOptions(req)
		result, err := eng.Ask(ctx, req.Question, opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, AskResponse{
			Answer: result.Answer,
			Refs:   refsToDTO(result.Refs),
			Mode:   modeRAG,
		})

	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid mode %q (want agent or rag)", req.Mode))
	}
}

// AskStream handles POST /api/ask/stream as Server-Sent Events (text/event-stream).
// Event types: progress | step | content | done | error (see StreamEvent / docs/111).
func (a *API) AskStream(w http.ResponseWriter, r *http.Request) {
	req, err := parseAskRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	eng, _, err := a.resolveEngine(r, req.Wiki)
	if err != nil {
		writeWikiError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func(ev StreamEvent) bool {
		return writeSSE(w, flusher, ev) == nil
	}

	mode := normalizeMode(req.Mode)
	switch mode {
	case modeAgent:
		streamAgent(r, eng, req, send)
	case modeRAG:
		streamRAG(r, eng, req, send)
	default:
		send(StreamEvent{Type: "error", Error: fmt.Sprintf("invalid mode %q (want agent or rag)", req.Mode)})
	}
}

func streamAgent(r *http.Request, eng QueryService, req *AskRequest, send func(StreamEvent) bool) {
	opts := agentOptions(req)
	opts.OnProgress = func(p agent.Progress) {
		send(StreamEvent{
			Type:   "progress",
			Phase:  string(p.Phase),
			Step:   p.Step,
			Action: p.Action,
			Args:   p.Args,
		})
	}
	opts.OnStep = func(s agent.Step) {
		dto := stepToDTO(s)
		send(StreamEvent{Type: "step", StepData: &dto, Step: s.Index})
	}

	result, err := eng.AskAgent(r.Context(), req.Question, opts)
	if err != nil {
		send(StreamEvent{Type: "error", Error: err.Error()})
		return
	}
	send(StreamEvent{
		Type:      "done",
		Answer:    result.Answer,
		Refs:      refsToDTO(result.Refs),
		Truncated: result.Truncated,
		Mode:      modeAgent,
	})
}

func streamRAG(r *http.Request, eng QueryService, req *AskRequest, send func(StreamEvent) bool) {
	opts := ragOptions(req)
	ch, err := eng.AskStream(r.Context(), req.Question, opts)
	if err != nil {
		send(StreamEvent{Type: "error", Error: err.Error()})
		return
	}

	var answer strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			send(StreamEvent{Type: "error", Error: chunk.Error.Error()})
			return
		}
		if chunk.Done {
			send(StreamEvent{
				Type:   "done",
				Answer: answer.String(),
				Refs:   refsToDTO(chunk.Refs),
				Mode:   modeRAG,
			})
			return
		}
		if chunk.Content != "" {
			answer.WriteString(chunk.Content)
			if !send(StreamEvent{Type: "content", Content: chunk.Content}) {
				return
			}
		}
	}
}

// --- helpers ---

func parseAskRequest(r *http.Request) (*AskRequest, error) {
	var req AskRequest
	if err := decodeJSON(r, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return nil, fmt.Errorf("question is required")
	}
	req.Wiki = strings.TrimSpace(req.Wiki)
	return &req, nil
}

func normalizeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return modeAgent
	}
	return mode
}

func agentOptions(req *AskRequest) *agent.Options {
	opts := &agent.Options{
		MaxSteps: req.MaxSteps,
		WallTime: agent.DefaultMaxWallTime,
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = agent.DefaultMaxSteps
	}
	return opts
}

func ragOptions(req *AskRequest) *query.AskOptions {
	opts := &query.AskOptions{
		TopN:     req.TopN,
		Effort:   parseEffort(req.Effort),
		Evidence: query.ParseEvidenceMode(req.Evidence),
	}
	if opts.TopN <= 0 {
		opts.TopN = query.DefaultTopN
	}
	return opts
}

func parseEffort(s string) wiki.SearchEffort {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "balanced":
		return wiki.SearchEffortBalanced
	case "thorough":
		return wiki.SearchEffortThorough
	default:
		return wiki.SearchEffortFast
	}
}

// writeSSE encodes one event as SSE data frame.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev StreamEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
