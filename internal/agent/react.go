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
	// Save writes the final Q&A as a synthesis page when true.
	Save bool
	// OnStep is an optional callback after each step (for CLI progress).
	OnStep func(step Step)
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
}

// Result is the agent answer.
type Result struct {
	Answer    string
	Citations []Citation
	Steps     []Step
	Truncated bool // true if stopped due to budget
}

// Citation references evidence used in the answer.
type Citation struct {
	Title string `json:"title,omitempty"`
	Path  string `json:"path,omitempty"`
	Layer string `json:"layer,omitempty"` // wiki | raw | code
}

// decision is the structured LLM output for one step.
type decision struct {
	Thought     string         `json:"thought"`
	Action      string         `json:"action"`
	Args        map[string]any `json:"args"`
	FinalAnswer string         `json:"final_answer"`
	Citations   []Citation     `json:"citations"`
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
	if opts == nil {
		opts = &Options{}
	}
	maxSteps := opts.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 12
	}
	maxRead := opts.MaxReadBytes
	if maxRead <= 0 {
		maxRead = tools.DefaultMaxReadBytes
	}
	wall := opts.WallTime
	if wall <= 0 {
		wall = 120 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, wall)
	defer cancel()

	roots := opts.Roots
	if len(roots) == 0 {
		roots = []string{e.wiki.WikiDir(), e.wiki.RawDir(), e.wiki.Root()}
	}
	sb, err := tools.NewSandbox(roots, maxRead)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}

	reg := tools.NewRegistry()
	tools.RegisterKnowledgeTools(reg, e.wiki)
	tools.RegisterFileTools(reg, sb)
	tools.RegisterCodeTools(reg, sb)

	if e.tracer != nil {
		e.tracer.Begin("agent", "question", question, "max_steps", maxSteps, "tools", len(reg.Names()))
		defer e.tracer.End()
	}

	var transcript []string
	var steps []Step
	consecutiveParseErrors := 0
	const maxConsecutiveParseErrors = 2
	sys := buildSystemPrompt(reg)

	for step := 0; step < maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return &Result{
				Answer:    partialAnswer(transcript, "stopped: time budget exhausted"),
				Steps:     steps,
				Truncated: true,
			}, nil
		}

		messages := buildMessages(sys, question, transcript)
		if e.tracer != nil {
			e.tracer.Begin("agent_step", "step", step+1)
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

		dec, err := parseDecision(resp.Content)
		if err != nil {
			consecutiveParseErrors++
			// Give the model one chance by feeding the parse error as observation.
			// But if the model repeatedly fails to produce valid JSON (e.g. context
			// overflow on a small local model), cut our losses instead of burning
			// all remaining steps.
			obs := fmt.Sprintf("ERROR: could not parse decision JSON: %v\nRaw:\n%s\nRespond with valid JSON only.", err, truncate(resp.Content, 800))
			transcript = append(transcript, formatTurn("(parse_error)", "none", nil, obs))
			steps = append(steps, Step{
				Index: step + 1, Thought: "parse_error", Observation: obs, Duration: time.Since(start),
			})
			if e.tracer != nil {
				e.tracer.End("parse_error", true)
			}
			if consecutiveParseErrors >= maxConsecutiveParseErrors {
				ans := partialAnswer(transcript, "Agent stopped: LLM repeatedly failed to produce valid JSON. The model may have run out of context — try a more capable model or a narrower question.")
				return &Result{
					Answer:    ans,
					Steps:     steps,
					Truncated: true,
				}, nil
			}
			continue
		}
		consecutiveParseErrors = 0

		// Final answer?
		if strings.TrimSpace(dec.FinalAnswer) != "" {
			st := Step{
				Index: step + 1, Thought: dec.Thought, Final: true, Duration: time.Since(start),
			}
			steps = append(steps, st)
			if opts.OnStep != nil {
				opts.OnStep(st)
			}
			if e.tracer != nil {
				e.tracer.End("final", true, "answer_chars", len(dec.FinalAnswer))
			}
			result := &Result{
				Answer:    dec.FinalAnswer,
				Citations: dec.Citations,
				Steps:     steps,
			}
			if opts.Save {
				if err := e.saveSynthesis(question, result); err != nil {
					return result, fmt.Errorf("saving synthesis: %w", err)
				}
			}
			return result, nil
		}

		if dec.Action == "" {
			obs := "ERROR: no action and no final_answer. Provide one or the other."
			transcript = append(transcript, formatTurn(dec.Thought, "", nil, obs))
			steps = append(steps, Step{Index: step + 1, Thought: dec.Thought, Observation: obs, Duration: time.Since(start)})
			if e.tracer != nil {
				e.tracer.End("missing_action", true)
			}
			continue
		}

		if e.tracer != nil {
			e.tracer.Begin("tool", "name", dec.Action)
		}
		obs, execErr := reg.Exec(ctx, dec.Action, dec.Args, maxRead)
		if execErr != nil {
			obs = fmt.Sprintf("ERROR: %v", execErr)
		}
		if e.tracer != nil {
			e.tracer.End("obs_bytes", len(obs), "error", execErr != nil)
		}

		turn := formatTurn(dec.Thought, dec.Action, dec.Args, obs)
		transcript = append(transcript, turn)
		st := Step{
			Index:       step + 1,
			Thought:     dec.Thought,
			Action:      dec.Action,
			Args:        dec.Args,
			Observation: truncate(obs, 500),
			Duration:    time.Since(start),
		}
		steps = append(steps, st)
		if opts.OnStep != nil {
			opts.OnStep(st)
		}
		if e.tracer != nil {
			e.tracer.End("action", dec.Action, "obs_bytes", len(obs))
		}
	}

	// Budget exhausted
	ans := partialAnswer(transcript, "Agent reached max_steps without a final_answer. Summarizing available evidence is incomplete.")
	return &Result{
		Answer:    ans,
		Steps:     steps,
		Truncated: true,
	}, nil
}

func buildSystemPrompt(reg *tools.Registry) string {
	return fmt.Sprintf(`You are Ruminate's exploration agent. You answer questions by gathering evidence with tools (ReAct).

## Dual truth
- **Wiki (Synthesis)**: compiled understanding — start here (wiki_search / wiki_read).
- **Raw (Evidence)**: archived originals — use when wiki is thin, contradictory, or the user needs precise quotes (raw_list_sources → raw_read / raw_search).
- **Code**: under configured roots — use file_grep, symbol_search, read_enclosing (syntactic Go via go/ast; NOT type-checked; not gopls). Prefer read_enclosing over whole-file reads.

## Rules
1. Prefer L1 wiki first; escalate to raw/code only when needed.
2. Default is READ-ONLY. Never invent tool observations — only use what tools return.
3. symbol_search / tree-sitter-style results may have multiple candidates; do not pretend uniqueness.
4. When evidence is insufficient, say so in final_answer.
5. Cite paths and wiki titles in the answer.

## Response format (STRICT JSON only — no markdown fences, no prose outside JSON)

Either call a tool:
{"thought":"...","action":"<tool_name>","args":{...}}

Or finish:
{"thought":"...","final_answer":"...","citations":[{"title":"...","path":"...","layer":"wiki|raw|code"}]}

## Available tools
%s
`, reg.SchemaJSON())
}

func buildMessages(sys, question string, transcript []string) []llm.Message {
	var b strings.Builder
	b.WriteString("Question: ")
	b.WriteString(question)
	b.WriteString("\n\n")
	if len(transcript) > 0 {
		b.WriteString("## Transcript so far\n\n")
		for i, t := range transcript {
			fmt.Fprintf(&b, "### Step %d\n%s\n\n", i+1, t)
		}
	}
	b.WriteString("Decide the next action or provide final_answer as JSON.")
	return []llm.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: b.String()},
	}
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
	s := strings.TrimSpace(raw)
	// Strip markdown fences if the model ignored instructions
	if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if strings.HasPrefix(s, "json") {
			s = s[4:]
		}
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}
	// Extract outermost JSON object
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}

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

func (e *Explorer) saveSynthesis(question string, result *Result) error {
	title := fmt.Sprintf("Q&A: %s", truncate(question, 60))
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "**Question**: %s\n\n", question)
	b.WriteString("## Answer\n\n")
	b.WriteString(result.Answer)
	b.WriteString("\n\n## Citations\n\n")
	for _, c := range result.Citations {
		fmt.Fprintf(&b, "- [%s] %s (%s)\n", c.Layer, c.Title, c.Path)
	}
	if len(result.Steps) > 0 {
		b.WriteString("\n## Agent trace\n\n")
		for _, s := range result.Steps {
			if s.Final {
				fmt.Fprintf(&b, "- step %d: final_answer\n", s.Index)
				continue
			}
			fmt.Fprintf(&b, "- step %d: %s\n", s.Index, s.Action)
		}
	}
	content := wiki.WithSources(b.String(), title, wiki.PageTypeSynthesis, nil)
	existing, err := e.wiki.Read(title, wiki.PageTypeSynthesis)
	if err != nil {
		_, err = e.wiki.Create(title, wiki.PageTypeSynthesis, content)
		return err
	}
	_, err = e.wiki.Update(existing.Title, wiki.PageTypeSynthesis, content)
	return err
}
