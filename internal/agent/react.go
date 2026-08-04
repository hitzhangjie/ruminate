// Package agent implements the embedded ReAct explorer (docs/109).
// Thought → Action(tool) → Observation loop with budget and path sandbox.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hitzhangjie/ruminate/internal/agent/tools"
	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/jsonx"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/trace"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// Options configures a ReAct run.
type Options struct {
	// MaxSteps caps Thought→Action iterations (default 12).
	MaxSteps int
	// MaxReadBytes caps tool observations (default 64KiB).
	MaxReadBytes int
	// WallTime is the total wall-clock budget (default 120s).
	WallTime time.Duration
	// Roots are filesystem roots the agent may read (wiki, raw, code).
	// If empty, wiki root (wiki/ + raw/) is used.
	Roots []string
	// OnStep is an optional callback after each step (for CLI progress).
	OnStep func(step Step)
}

const DefaultMaxSteps = 32
const DefaultMaxWallTime = 120 * time.Second

// defaultOptions returns Options populated with sensible defaults.
var defaultOptions = Options{
	MaxSteps:     DefaultMaxSteps,
	MaxReadBytes: tools.DefaultMaxReadBytes,
	WallTime:     DefaultMaxWallTime,
}

// Step is one ReAct turn for tracing/UI.
type Step struct {
	Index       int
	Thought     string
	Action      string
	Args        map[string]any
	Observation string
	Duration    time.Duration
	Final       bool
	// PromptTokens / CompletionTokens for the LLM decide call this step.
	// Zero when the provider omits usage (some local backends).
	PromptTokens     int
	CompletionTokens int
	// PromptChars is the approximate size of the decide-request user+system payload.
	// Useful when PromptTokens is 0 (fallback estimate: chars/3).
	PromptChars int
}

// Result is the agent answer.
type Result struct {
	Answer    string
	Refs      []wiki.Ref
	Steps     []Step
	Truncated bool // true if stopped due to budget
	// TotalPromptTokens / TotalCompletionTokens sum LLM usage across steps.
	// Zero components mean the provider did not report usage for those steps.
	TotalPromptTokens     int
	TotalCompletionTokens int
	// TotalPromptChars sums PromptChars across decide calls (always available).
	TotalPromptChars int
}

// decision is the structured LLM output for one step.
type decision struct {
	Thought     string         `json:"thought"`
	Action      string         `json:"action"`
	Args        map[string]any `json:"args"`
	FinalAnswer string         `json:"final_answer"`
	Refs        []wiki.Ref     `json:"references"`
}

// Explorer is the ReAct control plane.
type Explorer struct {
	wiki        *wiki.Manager
	llmProvider llm.LLMProvider
	llmCfg      config.LLMConfig
	tracer      *trace.Tracer
}

// NewExplorer creates an Explorer bound to a wiki manager and LLM.
func NewExplorer(mgr *wiki.Manager, provider llm.LLMProvider, llmCfg config.LLMConfig) *Explorer {
	return &Explorer{
		wiki:        mgr,
		llmProvider: provider,
		llmCfg:      llmCfg,
	}
}

// SetTracer attaches a pipeline tracer.
func (e *Explorer) SetTracer(tr *trace.Tracer) {
	e.tracer = tr
}

// Run executes the ReAct loop for the given question.
func (e *Explorer) Run(ctx context.Context, question string, opts *Options) (*Result, error) {
	if e.llmProvider == nil {
		return nil, fmt.Errorf("no LLM provider configured")
	}
	oo := defaultOptions
	if opts != nil {
		if oo.MaxSteps < opts.MaxSteps {
			oo.MaxSteps = opts.MaxSteps
		}
		if oo.MaxReadBytes < opts.MaxReadBytes {
			oo.MaxReadBytes = opts.MaxReadBytes
		}
		if oo.WallTime < opts.WallTime {
			oo.WallTime = opts.WallTime
		}
	}

	ctx, cancel := context.WithTimeout(ctx, oo.WallTime)
	defer cancel()

	// Always include wiki, raw, and root directories so the agent can
	// read its own knowledge base. Caller-provided Roots are additional.
	roots := append(
		[]string{e.wiki.WikiDir(), e.wiki.RawDir(), e.wiki.Root()},
		opts.Roots...,
	)
	sb, err := tools.NewSandbox(roots, oo.MaxReadBytes)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}

	reg := tools.NewRegistry()
	tools.RegisterKnowledgeTools(reg, e.wiki)
	tools.RegisterFileTools(reg, sb)
	tools.RegisterCodeTools(reg, sb)

	if e.tracer != nil {
		e.tracer.Begin("agent", "question", question, "max_steps", oo.MaxSteps, "tools", len(reg.Names()))
		defer e.tracer.End()
	}

	var transcript []string
	var steps []Step
	var totalPrompt, totalCompletion, totalPromptChars int
	consecutiveParseErrors := 0
	const maxConsecutiveParseErrors = 2
	sys := buildSystemPrompt(reg)

	for step := 0; step < oo.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return &Result{
				Answer:                partialAnswer(transcript, "stopped: time budget exhausted"),
				Steps:                 steps,
				Truncated:             true,
				TotalPromptTokens:     totalPrompt,
				TotalCompletionTokens: totalCompletion,
				TotalPromptChars:      totalPromptChars,
			}, nil
		}

		messages := buildMessages(sys, question, transcript)
		promptChars := messagesChars(messages)
		if e.tracer != nil {
			e.tracer.Begin("agent_step", "step", step+1, "prompt_chars", promptChars)
		}

		start := time.Now()
		resp, err := e.llmProvider.Chat(ctx, messages, &llm.ChatOptions{
			Temperature: e.llmCfg.Temperature,
		})
		if err != nil {
			if e.tracer != nil {
				e.tracer.Error(err)
			}
			return nil, fmt.Errorf("LLM decide (step %d): %w", step+1, err)
		}

		pt, ct := resp.Usage.PromptTokens, resp.Usage.CompletionTokens
		totalPrompt += pt
		totalCompletion += ct
		totalPromptChars += promptChars

		dec, err := parseDecision(resp.Content)
		if err != nil {
			consecutiveParseErrors++
			// Give the model one chance by feeding the parse error as observation.
			// But if the model repeatedly fails to produce valid JSON (e.g. context
			// overflow on a small local model), cut our losses instead of burning
			// all remaining steps.
			obs := fmt.Sprintf("ERROR: could not parse decision JSON: %v\nRaw:\n%s\nRespond with valid JSON only.", err, truncate(resp.Content, 800))
			transcript = append(transcript, formatTurn("(parse_error)", "none", nil, obs))
			st := Step{
				Index: step + 1, Thought: "parse_error", Observation: obs, Duration: time.Since(start),
				PromptTokens: pt, CompletionTokens: ct, PromptChars: promptChars,
			}
			steps = append(steps, st)
			if opts != nil && opts.OnStep != nil {
				opts.OnStep(st)
			}
			if e.tracer != nil {
				e.tracer.End("parse_error", true, "prompt_tok", pt, "completion_tok", ct)
			}
			if consecutiveParseErrors >= maxConsecutiveParseErrors {
				ans := partialAnswer(transcript, "Agent stopped: LLM repeatedly failed to produce valid JSON. The model may have run out of context — try a more capable model or a narrower question.")
				return &Result{
					Answer:                ans,
					Steps:                 steps,
					Truncated:             true,
					TotalPromptTokens:     totalPrompt,
					TotalCompletionTokens: totalCompletion,
					TotalPromptChars:      totalPromptChars,
				}, nil
			}
			continue
		}
		consecutiveParseErrors = 0

		// Final answer?
		if strings.TrimSpace(dec.FinalAnswer) != "" {
			st := Step{
				Index: step + 1, Thought: dec.Thought, Final: true, Duration: time.Since(start),
				PromptTokens: pt, CompletionTokens: ct, PromptChars: promptChars,
			}
			steps = append(steps, st)
			if opts != nil && opts.OnStep != nil {
				opts.OnStep(st)
			}
			if e.tracer != nil {
				e.tracer.End("final", true, "answer_chars", len(dec.FinalAnswer),
					"prompt_tok", pt, "completion_tok", ct)
			}
			return &Result{
				Answer:                dec.FinalAnswer,
				Refs:                  dec.Refs,
				Steps:                 steps,
				TotalPromptTokens:     totalPrompt,
				TotalCompletionTokens: totalCompletion,
				TotalPromptChars:      totalPromptChars,
			}, nil
		}

		if dec.Action == "" {
			obs := "ERROR: no action and no final_answer. Provide one or the other."
			transcript = append(transcript, formatTurn(dec.Thought, "", nil, obs))
			st := Step{
				Index: step + 1, Thought: dec.Thought, Observation: obs, Duration: time.Since(start),
				PromptTokens: pt, CompletionTokens: ct, PromptChars: promptChars,
			}
			steps = append(steps, st)
			if opts != nil && opts.OnStep != nil {
				opts.OnStep(st)
			}
			if e.tracer != nil {
				e.tracer.End("missing_action", true)
			}
			continue
		}

		if e.tracer != nil {
			e.tracer.Begin("tool", "name", dec.Action, "arg", traceArgSummary(dec.Action, dec.Args))
		}
		obs, execErr := reg.Exec(ctx, dec.Action, dec.Args, oo.MaxReadBytes)
		if execErr != nil {
			obs = fmt.Sprintf("ERROR: %v", execErr)
		}
		if e.tracer != nil {
			e.tracer.End("obs_bytes", len(obs), "error", execErr != nil)
		}

		turn := formatTurn(dec.Thought, dec.Action, dec.Args, obs)
		transcript = append(transcript, turn)
		st := Step{
			Index:            step + 1,
			Thought:          dec.Thought,
			Action:           dec.Action,
			Args:             dec.Args,
			Observation:      truncate(obs, 500),
			Duration:         time.Since(start),
			PromptTokens:     pt,
			CompletionTokens: ct,
			PromptChars:      promptChars,
		}
		steps = append(steps, st)
		if opts != nil && opts.OnStep != nil {
			opts.OnStep(st)
		}
		if e.tracer != nil {
			e.tracer.End("action", dec.Action, "obs_bytes", len(obs),
				"prompt_tok", pt, "completion_tok", ct, "prompt_chars", promptChars)
		}
	}

	// Budget exhausted
	ans := partialAnswer(transcript, "Agent reached max_steps without a final_answer. Summarizing available evidence is incomplete.")
	return &Result{
		Answer:                ans,
		Steps:                 steps,
		Truncated:             true,
		TotalPromptTokens:     totalPrompt,
		TotalCompletionTokens: totalCompletion,
		TotalPromptChars:      totalPromptChars,
	}, nil
}

func buildSystemPrompt(reg *tools.Registry) string {
	return fmt.Sprintf(`You are Ruminate's exploration agent. You answer questions by gathering evidence with tools (ReAct).

## Dual truth
- **Wiki (Synthesis)**: compiled understanding — navigate the catalog, then open pages.
- **Raw (Evidence)**: archived originals — use when wiki is thin, contradictory, or the user needs precise quotes (raw_list_sources → raw_read / raw_search).
- **External files**: under configured agent roots — code, prose, Markdown notes, etc. list_dir first, then file_grep / file_read. Use code tools (symbol_search, read_enclosing, ast_outline) only after you confirm Go (or other) source is present.

## Exploration strategy (wiki)
You are an explorer, NOT a single-shot RAG pipeline. Prefer precise drill-down over dumping many candidates into context:
1. **wiki_index** first (optionally with filter) — catalog of titles/paths/summaries. This is the table of contents.
2. **wiki_read** promising pages from the index.
3. **wiki_search** only when you need keyword/BM25 lookup (cheap FTS; no embeddings). Do not treat it as a full ask pipeline.
4. Escalate to raw_* or external roots only when wiki is insufficient.

## Rules
0. When exploring a new root or directory, always list_dir first. Choose tools based on what you see.
1. Prefer L1 wiki first; escalate to raw/external files only when needed.
2. Default is READ-ONLY. Never invent tool observations — only use what tools return.
3. symbol_search / tree-sitter-style results may have multiple candidates; do not pretend uniqueness.
4. When evidence is insufficient, say so in final_answer.
5. Cite paths and wiki titles in the answer.
6. If a search returns no results, vary: different keywords, wiki_index filter, read promising files, drill subdirs. Do not retry the same empty search.
7. Keep context lean: open only the pages you need; summarize mentally and answer as soon as evidence is enough.

## Response format (STRICT JSON only — no markdown fences, no prose outside JSON)

Either call a tool:
{"thought":"...","action":"<tool_name>","args":{...}}

Or finish:
{"thought":"...","final_answer":"...","references":[{"title":"...","path":"...","layer":"wiki|raw|code"}]}

## Available tools
%s
`, reg.SchemaJSON())
}

// transcriptKeepFull is how many recent steps keep full observations in the
// next decide prompt. Older steps are compacted to thought+action+short obs
// so long explorations do not blow the context window.
const transcriptKeepFull = 4

// transcriptOldObsMax is the max observation chars retained for compacted steps.
const transcriptOldObsMax = 400

func buildMessages(sys, question string, transcript []string) []llm.Message {
	var b strings.Builder
	b.WriteString("Question: ")
	b.WriteString(question)
	b.WriteString("\n\n")
	if len(transcript) > 0 {
		b.WriteString("## Transcript so far\n\n")
		fullFrom := 0
		if len(transcript) > transcriptKeepFull {
			fullFrom = len(transcript) - transcriptKeepFull
		}
		for i, t := range transcript {
			body := t
			if i < fullFrom {
				body = compactTurn(t)
			}
			fmt.Fprintf(&b, "### Step %d\n%s\n\n", i+1, body)
		}
	}
	b.WriteString("Decide the next action or provide final_answer as JSON.")
	return []llm.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: b.String()},
	}
}

// compactTurn keeps Thought/Action and truncates Observation for old steps.
func compactTurn(turn string) string {
	const marker = "Observation:\n"
	idx := strings.Index(turn, marker)
	if idx < 0 {
		return truncate(turn, transcriptOldObsMax+80)
	}
	head := turn[:idx+len(marker)]
	obs := turn[idx+len(marker):]
	return head + truncate(obs, transcriptOldObsMax)
}

func messagesChars(messages []llm.Message) int {
	n := 0
	for _, m := range messages {
		n += len(m.Content)
	}
	return n
}

func formatTurn(thought, action string, args map[string]any, obs string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Thought: %s\n", thought)
	if action != "" {
		argsJSON, _ := json.Marshal(args)
		fmt.Fprintf(&b, "Action: %s %s\n", action, string(argsJSON))
	}
	fmt.Fprintf(&b, "Observation:\n%s", obs)
	return b.String()
}

func parseDecision(raw string) (*decision, error) {
	// Use robust JSON cleaning: strip fences, extract object with proper
	// brace matching (string-aware, not LastIndex), sanitize real control
	// characters inside string values (common Ollama defect).
	s := jsonx.CleanObject(raw)

	var d decision
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&d); err != nil {
		return nil, err
	}
	// Normalize args nil
	if d.Args == nil {
		d.Args = map[string]any{}
	}
	return &d, nil
}

func partialAnswer(transcript []string, note string) string {
	var b strings.Builder
	b.WriteString(note)
	b.WriteString("\n\n")
	if len(transcript) == 0 {
		b.WriteString("No tool observations were collected.")
		return b.String()
	}
	b.WriteString("Collected evidence (last steps):\n")
	start := 0
	if len(transcript) > 3 {
		start = len(transcript) - 3
	}
	for i := start; i < len(transcript); i++ {
		fmt.Fprintf(&b, "\n--- step %d ---\n%s\n", i+1, truncate(transcript[i], 1200))
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// traceArgSummary extracts the most informative argument from a tool call
// for compact trace output (search query, file path, grep pattern, etc.).
func traceArgSummary(action string, args map[string]any) string {
	switch action {
	case "wiki_search", "raw_search":
		return stringFromArgs(args, "query")
	case "wiki_index":
		if f := stringFromArgs(args, "filter"); f != "" {
			return f
		}
		return "all"
	case "wiki_read", "raw_read", "file_read", "wiki_links", "list_dir", "ast_outline":
		return stringFromArgs(args, "path")
	case "raw_list_sources":
		if p := stringFromArgs(args, "path"); p != "" {
			return p
		}
		return "all"
	case "file_grep":
		return stringFromArgs(args, "pattern")
	case "symbol_search":
		return stringFromArgs(args, "name")
	case "read_enclosing":
		path := stringFromArgs(args, "path")
		line := intFromArgs(args, "line")
		if line > 0 {
			return fmt.Sprintf("%s:%d", path, line)
		}
		return path
	default:
		return ""
	}
}

// stringFromArgs extracts a string value from tool args.
func stringFromArgs(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// intFromArgs extracts an int value from tool args (accepts float64 from JSON).
func intFromArgs(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}
