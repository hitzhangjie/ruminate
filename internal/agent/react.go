// Package agent implements the embedded ReAct explorer (docs/109).
// Thought → Action(tool) → Observation loop with budget and path sandbox.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// OnStep is an optional callback after each step completes (for CLI / UI).
	OnStep func(step Step)
	// OnProgress is an optional callback for in-flight phases (decide / tool).
	// Used by live terminal UIs for spinners; safe to leave nil.
	OnProgress func(p Progress)
	// CollectPrompts attaches SystemPrompt / UserPrompt / LLMRaw to each Step
	// so callers can show full decide-round observability (e.g. CLI -v).
	// Slightly more memory per step; default false.
	CollectPrompts bool
	// ObservationLimit is max chars kept on Step.Observation for UI/trace.
	// Full tool output still goes into the LLM transcript. 0 = default (8KiB).
	ObservationLimit int
}

// ProgressPhase is an in-flight agent activity (before a Step is complete).
type ProgressPhase string

const (
	// ProgressDecide: LLM is choosing the next action / final answer.
	ProgressDecide ProgressPhase = "decide"
	// ProgressTool: a tool is executing after a decision was parsed.
	ProgressTool ProgressPhase = "tool"
)

// Progress is a mid-step status event for live UIs (spinner / status line).
type Progress struct {
	Phase  ProgressPhase
	Step   int // 1-based step index currently in flight
	Action string
	Args   map[string]any
}

const DefaultMaxSteps = 64
const DefaultMaxWallTime = 10 * time.Minute

// defaultStepObservationLimit is how much tool output is retained on Step for display.
// Full observations remain in the transcript fed back to the LLM.
const defaultStepObservationLimit = 8 * 1024

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
	// ObsBytes is the full observation size before truncation for Step.Observation.
	ObsBytes int
	Duration time.Duration
	Final    bool
	// FinalAnswer is set when Final is true (the model’s answer text).
	FinalAnswer string
	// PromptTokens / CompletionTokens for the LLM decide call this step.
	// Zero when the provider omits usage (some local backends).
	PromptTokens     int
	CompletionTokens int
	// PromptChars is the approximate size of the decide-request user+system payload.
	// Useful when PromptTokens is 0 (fallback estimate: chars/3).
	PromptChars int
	// ParseDumpPath is set on parse_error steps: absolute path to the dumped
	// raw LLM response for offline investigation.
	ParseDumpPath string

	// --- Optional decide-round detail (populated when Options.CollectPrompts) ---

	// SystemPrompt is the system message for this decide call (usually only on step 1).
	SystemPrompt string
	// UserPrompt is the user message (question + transcript + decide instruction).
	UserPrompt string
	// LLMRaw is the raw model response content before JSON parse.
	LLMRaw string
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
		oo.CollectPrompts = opts.CollectPrompts
		if opts.ObservationLimit > 0 {
			oo.ObservationLimit = opts.ObservationLimit
		}
	}
	obsLimit := oo.ObservationLimit
	if obsLimit <= 0 {
		obsLimit = defaultStepObservationLimit
	}

	ctx, cancel := context.WithTimeout(ctx, oo.WallTime)
	defer cancel()

	// Always include wiki, raw, and workspace root so the agent can read its
	// knowledge base. Caller-provided Roots (--agent-root) are additional.
	// Labels are injected into the system prompt so the model knows concrete
	// paths (sandbox alone is not enough — the LLM never saw them before).
	var extraRoots []string
	if opts != nil {
		extraRoots = opts.Roots
	}
	rootDescs := buildRootDescriptors(e.wiki, extraRoots)
	roots := make([]string, len(rootDescs))
	for i, d := range rootDescs {
		roots[i] = d.Path
	}
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
	sys := buildSystemPrompt(reg, rootDescs)
	emitStep := func(st Step) {
		steps = append(steps, st)
		if opts != nil && opts.OnStep != nil {
			opts.OnStep(st)
		}
	}
	emitProgress := func(p Progress) {
		if opts != nil && opts.OnProgress != nil {
			opts.OnProgress(p)
		}
	}

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
		emitProgress(Progress{Phase: ProgressDecide, Step: step + 1})
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

		// Base fields shared by every step variant this iteration.
		base := Step{
			Index:            step + 1,
			Duration:         time.Since(start),
			PromptTokens:     pt,
			CompletionTokens: ct,
			PromptChars:      promptChars,
		}
		if oo.CollectPrompts {
			// System prompt is large (tool schema); attach only on first decide call.
			if step == 0 {
				base.SystemPrompt = sys
			}
			if len(messages) > 1 {
				base.UserPrompt = messages[len(messages)-1].Content
			}
			base.LLMRaw = resp.Content
		}

		dec, err := parseDecision(resp.Content)
		if err != nil {
			consecutiveParseErrors++
			// Dump full raw + cleaned content for offline investigation
			// (truncated in the model observation is not enough to diagnose
			// prompt / format compliance issues).
			cleaned := jsonx.CleanObject(resp.Content)
			dumpPath, dumpErr := dumpParseError(e.wiki.Root(), step+1, resp.Content, cleaned, err)
			// Give the model one chance by feeding the parse error as observation.
			// But if the model repeatedly fails to produce valid JSON (e.g. context
			// overflow on a small local model), cut our losses instead of burning
			// all remaining steps.
			obs := fmt.Sprintf("ERROR: could not parse decision JSON: %v\nRaw:\n%s\nRespond with valid JSON only.", err, truncate(resp.Content, 800))
			if dumpPath != "" {
				obs = fmt.Sprintf("ERROR: could not parse decision JSON: %v\n(full dump: %s)\nRaw:\n%s\nRespond with valid JSON only.", err, dumpPath, truncate(resp.Content, 800))
			} else if dumpErr != nil {
				obs = fmt.Sprintf("ERROR: could not parse decision JSON: %v\n(dump failed: %v)\nRaw:\n%s\nRespond with valid JSON only.", err, dumpErr, truncate(resp.Content, 800))
			}
			transcript = append(transcript, formatTurn("(parse_error)", "none", nil, obs))
			st := base
			st.Thought = "parse_error"
			st.Observation = truncate(obs, obsLimit)
			st.ObsBytes = len(obs)
			st.ParseDumpPath = dumpPath
			// Duration was snapped before parse work; refresh.
			st.Duration = time.Since(start)
			emitStep(st)
			if e.tracer != nil {
				attrs := []any{"parse_error", true, "prompt_tok", pt, "completion_tok", ct}
				if dumpPath != "" {
					attrs = append(attrs, "dump", dumpPath)
				}
				e.tracer.End(attrs...)
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
			st := base
			st.Thought = dec.Thought
			st.Final = true
			st.FinalAnswer = dec.FinalAnswer
			st.Duration = time.Since(start)
			emitStep(st)
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
			st := base
			st.Thought = dec.Thought
			st.Observation = truncate(obs, obsLimit)
			st.ObsBytes = len(obs)
			st.Duration = time.Since(start)
			emitStep(st)
			if e.tracer != nil {
				e.tracer.End("missing_action", true)
			}
			continue
		}

		// Models (esp. via native tool_calls) invent namespaced names like
		// "repo_browser.list_dir". Resolve to a registered tool when possible
		// so logs and transcript use the canonical name.
		if resolved, ok := reg.ResolveName(dec.Action); ok {
			dec.Action = resolved
		}

		if e.tracer != nil {
			e.tracer.Begin("tool", "name", dec.Action, "arg", traceArgSummary(dec.Action, dec.Args))
		}
		emitProgress(Progress{
			Phase:  ProgressTool,
			Step:   step + 1,
			Action: dec.Action,
			Args:   dec.Args,
		})
		obs, execErr := reg.Exec(ctx, dec.Action, dec.Args, oo.MaxReadBytes)
		if execErr != nil {
			obs = fmt.Sprintf("ERROR: %v", execErr)
		}
		if e.tracer != nil {
			e.tracer.End("obs_bytes", len(obs), "error", execErr != nil)
		}

		turn := formatTurn(dec.Thought, dec.Action, dec.Args, obs)
		transcript = append(transcript, turn)
		st := base
		st.Thought = dec.Thought
		st.Action = dec.Action
		st.Args = dec.Args
		st.Observation = truncate(obs, obsLimit)
		st.ObsBytes = len(obs)
		st.Duration = time.Since(start)
		emitStep(st)
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

// rootDescriptor is one filesystem root the agent may read, with a human label
// for the system prompt (wiki / raw / workspace / --agent-root).
type rootDescriptor struct {
	Path  string
	Label string
}

// buildRootDescriptors lists sandbox roots in a stable order with labels.
// extra comes from Options.Roots / CLI --agent-root.
func buildRootDescriptors(mgr *wiki.Manager, extra []string) []rootDescriptor {
	out := []rootDescriptor{
		{Path: mgr.WikiDir(), Label: "wiki (synthesis Markdown pages)"},
		{Path: mgr.RawDir(), Label: "raw (evidence originals)"},
		{Path: mgr.Root(), Label: "wiki workspace root (index.md, raw/, wiki/, …)"},
	}
	seen := map[string]bool{}
	for _, d := range out {
		if a, err := filepath.Abs(d.Path); err == nil {
			seen[filepath.Clean(a)] = true
		}
	}
	for _, r := range extra {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = r
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, rootDescriptor{
			Path:  abs,
			Label: "extra root (--agent-root): explore with list_dir / file_grep / file_read",
		})
	}
	return out
}

func formatRootsForPrompt(roots []rootDescriptor) string {
	if len(roots) == 0 {
		return "(no filesystem roots configured)"
	}
	var b strings.Builder
	for i, d := range roots {
		fmt.Fprintf(&b, "%d. `%s`\n   role: %s\n", i+1, d.Path, d.Label)
	}
	return strings.TrimRight(b.String(), "\n")
}

func buildSystemPrompt(reg *tools.Registry, roots []rootDescriptor) string {
	return fmt.Sprintf(`You are Ruminate's exploration agent. You answer questions by gathering evidence with tools (ReAct).

## Dual truth
- **Wiki (Synthesis)**: compiled understanding — navigate the catalog, then open pages.
- **Raw (Evidence)**: archived originals — use when wiki is thin, contradictory, or the user needs precise quotes (raw_list_sources → raw_read / raw_search).
- **External / code roots**: absolute paths listed below (includes CLI --agent-root). list_dir first (pass the root path), then file_grep / file_read. Use code tools (symbol_search, read_enclosing, ast_outline) only after you confirm source files are present.

## Filesystem roots you MAY read (sandbox allow-list)
These are the only directories file tools can access. **Always use one of these absolute paths** (or a subpath under them) for list_dir / file_read / file_grep / code tools.
list_dir with empty path or path="." returns this list again.

%s

## Exploration strategy
You are an explorer, NOT a single-shot RAG pipeline. Prefer precise drill-down over dumping many candidates into context.

### Default ladder (follow in order; skip only when already enough evidence)
1. **wiki_index** — start with **no filter** (or a broad category word), not the user's exact jargon. Catalog = titles/paths/one-line summaries.
2. In **thought**, name 1–3 promising catalog lines (titles may use different words than the user). Then **wiki_read** those pages.
3. **wiki_search** only for keyword/BM25 lookup (cheap FTS; **no embeddings, no semantic expansion**). Treat empty FTS as "literal miss", not "topic absent".
4. If wiki is thin: **raw_list_sources** / **raw_search** / **raw_read**.
5. If still thin and extra roots exist: **list_dir** on the extra root absolute path → **file_grep** synonym patterns → **file_read** / **read_enclosing**.

### Term bridging (CRITICAL — user wording ≠ wiki titles)
Users often use acronyms, English, or slang; wiki titles use other phrasings. **Before** concluding "not in wiki", expand the question:

1. **Expand acronyms / jargon** in thought (examples, not exhaustive):
   - GMP → Go scheduler / Goroutine·Machine·Processor / runtime scheduling / 运行时调度 / goroutine 调度
   - GC → garbage collection / 垃圾回收 / write barrier / 写屏障 / concurrent mark
   - THP → transparent huge pages / 透明巨页 / 大页
   - RAG → retrieval / embedding / 检索 / 向量
2. **Bilingual + synonym queries**: try Chinese AND English forms (调度↔scheduler, 运行时↔runtime, 并发↔concurrency).
3. **Prefer opening near titles** over more empty searches: if the catalog shows anything about runtime / 调度 / scheduler / goroutine / 内存 / GC when the user asked about GMP/scheduling, **wiki_read it** even if the title does not contain "GMP".
4. After **wiki_index**, jot candidate titles in thought immediately (observations may be compacted later).

### When search / filter returns empty (do NOT give up)
After **one** empty result, change strategy — never repeat the same query/filter:
1. Drop filter → full **wiki_index** (or broader stem: 调度, runtime, memory, network…).
2. **wiki_search** with paraphrases from the term-bridge list (not the original string only).
3. **wiki_read** any plausible catalog hit from prior steps.
4. Escalate: raw_* then **list_dir + file_grep** on --agent-root paths with the same synonym set.
5. Only after this ladder may you say evidence is insufficient — and list what you tried (queries, paths).

**Forbidden**: final_answer claiming the wiki has nothing on the topic solely because wiki_search/filter for the user's exact words returned empty, while you never opened a near-title page or searched synonyms / agent-root.

## Rules
0. Filesystem: list_dir with a root absolute path from the list above (empty path only to re-list roots). path is required for a real directory listing once you know the path.
1. Prefer L1 wiki first; escalate to raw/external only when needed — but do escalate when wiki literal search fails.
2. READ-ONLY. Never invent tool observations — only use what tools return.
3. symbol_search / multi-candidate code results: do not pretend uniqueness.
4. When evidence is still insufficient after the ladder, say so honestly and cite attempts.
5. Cite paths and wiki titles in the answer.
6. Keep context lean: open pages you need; answer when evidence is enough — not before the ladder is exhausted on hard questions.
7. Do not retry the identical empty wiki_search / wiki_index filter.

## Response format (STRICT JSON only — no markdown fences, no prose outside JSON)

Put the decision in the **message content** as a single JSON object.
Do **not** use native function/tool_calls API fields — tools are invoked only via this JSON.

**action** must be exactly one of the available tool names below (e.g. list_dir).
Do not invent namespaced names like repo_browser.list_dir or tools.wiki_search.

Either call a tool:
{"thought":"...","action":"<tool_name>","args":{...}}

Or finish:
{"thought":"...","final_answer":"...","references":[{"title":"...","path":"...","layer":"wiki|raw|code"}]}

## Available tools
%s
`, formatRootsForPrompt(roots), reg.SchemaJSON())
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
	b.WriteString("Decide the next action or provide final_answer as JSON.\n")
	b.WriteString("If recent wiki_search/filter results were empty: expand synonyms/acronyms, drop filters, wiki_read near titles, or list_dir+file_grep on agent-root — do not final_answer \"not found\" on literal misses alone.")
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

// parseErrorDumpDir is the relative path under the wiki root where failed
// decision JSON is written for investigation.
const parseErrorDumpDir = "db/debug/parse_errors"

// dumpParseError writes the unparseable LLM response to
// {wikiRoot}/db/debug/parse_errors/ for offline investigation.
// Returns the absolute path of the dump file, or empty path + error.
func dumpParseError(wikiRoot string, step int, raw, cleaned string, parseErr error) (string, error) {
	if wikiRoot == "" {
		return "", fmt.Errorf("empty wiki root")
	}
	dir := filepath.Join(wikiRoot, parseErrorDumpDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir dump dir: %w", err)
	}

	// Unique filename: timestamp + step + short content hash-ish length marker.
	ts := time.Now().Format("20060102_150405.000")
	name := fmt.Sprintf("parse_error_%s_step%02d.txt", ts, step)
	path := filepath.Join(dir, name)

	var b strings.Builder
	b.WriteString("# Ruminate agent decision parse error dump\n")
	b.WriteString("# For investigating intermittent parse_error / JSON non-compliance.\n")
	fmt.Fprintf(&b, "time: %s\n", time.Now().Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "step: %d\n", step)
	fmt.Fprintf(&b, "parse_error: %v\n", parseErr)
	fmt.Fprintf(&b, "raw_bytes: %d\n", len(raw))
	fmt.Fprintf(&b, "cleaned_bytes: %d\n", len(cleaned))
	b.WriteString("\n")
	b.WriteString("===== RAW (exactly as returned by LLM) =====\n")
	b.WriteString(raw)
	if !strings.HasSuffix(raw, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n===== CLEANED (after jsonx.CleanObject) =====\n")
	b.WriteString(cleaned)
	if !strings.HasSuffix(cleaned, "\n") {
		b.WriteByte('\n')
	}
	// Also show a hex dump of first bytes if content looks truncated / binary-ish.
	if len(raw) > 0 {
		preview := raw
		if len(preview) > 64 {
			preview = preview[:64]
		}
		fmt.Fprintf(&b, "\n===== RAW first 64 bytes (hex) =====\n% x\n", []byte(preview))
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return "", fmt.Errorf("write dump: %w", err)
	}
	return path, nil
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
