// Package handler implements HTTP API handlers for the Ruminate server.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hitzhangjie/ruminate/internal/agent"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// --- Request / response DTOs ---

// AskRequest is the body of POST /api/ask and POST /api/ask/stream.
type AskRequest struct {
	Question string `json:"question"`
	// Wiki selects the knowledge base in multi-wiki mode (optional).
	Wiki string `json:"wiki,omitempty"`
	// Mode is "agent" (default ReAct) or "rag" (single-pass retrieval).
	Mode string `json:"mode,omitempty"`
	// MaxSteps caps ReAct iterations (agent mode only).
	MaxSteps int `json:"max_steps,omitempty"`
	// TopN is retrieval width for rag mode.
	TopN int `json:"top_n,omitempty"`
	// Effort is fast|balanced|thorough (rag mode only).
	Effort string `json:"effort,omitempty"`
	// Evidence is auto|raw|wiki (rag mode only).
	Evidence string `json:"evidence,omitempty"`
}

// RefDTO is a serializable citation without full page content.
type RefDTO struct {
	Title   string `json:"title"`
	Path    string `json:"path"`
	Layer   string `json:"layer,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// StepDTO is a compact agent step for JSON/SSE.
type StepDTO struct {
	Index            int            `json:"index"`
	Thought          string         `json:"thought,omitempty"`
	Action           string         `json:"action,omitempty"`
	Args             map[string]any `json:"args,omitempty"`
	Observation      string         `json:"observation,omitempty"`
	ObsBytes         int            `json:"obs_bytes,omitempty"`
	DurationMs       int64          `json:"duration_ms"`
	Final            bool           `json:"final,omitempty"`
	FinalAnswer      string         `json:"final_answer,omitempty"`
	PromptTokens     int            `json:"prompt_tokens,omitempty"`
	CompletionTokens int            `json:"completion_tokens,omitempty"`
}

// AskResponse is the non-streaming answer payload.
type AskResponse struct {
	Answer    string    `json:"answer"`
	Refs      []RefDTO  `json:"refs,omitempty"`
	Steps     []StepDTO `json:"steps,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Mode      string    `json:"mode"`
}

// StreamEvent is one SSE / NDJSON event for progressive ask UI.
// Type values: progress | step | content | done | error
type StreamEvent struct {
	Type string `json:"type"`

	// progress
	Phase  string         `json:"phase,omitempty"`
	Step   int            `json:"step,omitempty"`
	Action string         `json:"action,omitempty"`
	Args   map[string]any `json:"args,omitempty"`

	// step
	StepData *StepDTO `json:"step_data,omitempty"`

	// content (rag token stream)
	Content string `json:"content,omitempty"`

	// done
	Answer    string   `json:"answer,omitempty"`
	Refs      []RefDTO `json:"refs,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Mode      string   `json:"mode,omitempty"`

	// error
	Error string `json:"error,omitempty"`
}



// --- converters ---

func refsToDTO(refs []wiki.Ref) []RefDTO {
	if len(refs) == 0 {
		return nil
	}
	out := make([]RefDTO, len(refs))
	for i, r := range refs {
		layer := r.Layer
		if layer == "" {
			layer = "wiki"
		}
		out[i] = RefDTO{
			Title:   r.Title,
			Path:    r.Path,
			Layer:   layer,
			Snippet: r.Snippet,
		}
	}
	return out
}

func stepToDTO(s agent.Step) StepDTO {
	return StepDTO{
		Index:            s.Index,
		Thought:          s.Thought,
		Action:           s.Action,
		Args:             s.Args,
		Observation:      s.Observation,
		ObsBytes:         s.ObsBytes,
		DurationMs:       s.Duration.Round(time.Millisecond).Milliseconds(),
		Final:            s.Final,
		FinalAnswer:      s.FinalAnswer,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
	}
}

func stepsToDTO(steps []agent.Step) []StepDTO {
	if len(steps) == 0 {
		return nil
	}
	out := make([]StepDTO, len(steps))
	for i, s := range steps {
		out[i] = stepToDTO(s)
	}
	return out
}

// --- HTTP helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
