package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/agent"
	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/trace"
	"github.com/hitzhangjie/ruminate/internal/ui/agentview"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var (
	askNoStream  bool
	askTopN      int
	askEffort    string
	askEvidence  string
	askAgent     bool
	askMaxSteps  int
	askAgentRoot []string
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question and get AI-synthesized answer from wiki",
	Long: `Search relevant wiki pages and use LLM to synthesize
a comprehensive answer with references.

The ask pipeline (default):
  1. Search wiki pages using hybrid/FTS retrieval (L1 Synthesis)
  2. Auto-escalate to raw Evidence when needed (L2; configurable via --evidence)
  3. Build LLM prompt with context + question
  4. Stream the synthesized answer (or --no-stream)
  5. Answer includes references in [[page]] notation

Agent mode (--agent): multi-step ReAct exploration (docs/109, docs/111).
  Uses tools: wiki_*, raw_*, file_grep/read, symbol_search, read_enclosing.
  Default read-only; code intelligence is syntactic (go/ast), not gopls.

  On a TTY, progress uses a Claude Code–style live view (spinner + step cards).
  Non-TTY / pipes use a compact one-line timeline. Failed steps surface errors.
  With -v/--verbose, prints full Thought → Action → Observation plus
  decide-round prompts and raw LLM response (docs/111).

Examples:
  ruminate ask "What is RAG?"
  ruminate ask --evidence auto "原文默认超时是多少？"
  ruminate ask --agent "Reconcile 会不会阻塞？"
  ruminate ask --agent -v "…"   # full prompt + thought + action + observation
  ruminate ask --agent --agent-root /path/to/code "Where is Hello defined?"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question := strings.Join(args, " ")

		// Load configuration
		wikiName, _ := cmd.Flags().GetString("wiki")
		cfg, err := loadRuntimeConfig(wikiName)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// init tracer
		verbose, _ := cmd.Flags().GetBool("verbose")
		tracer := trace.New(verbose)
		defer tracer.Flush(os.Stderr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle Ctrl-C gracefully
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			<-sigCh
			cancel()
		}()

		// Create query engine once — shared by ask and agent paths.
		engine, err := query.NewEngine(cfg)
		if err != nil {
			return err
		}
		engine.SetTracer(tracer)

		// mode1: agent exploration mode
		if askAgent {
			return runAgent(ctx, engine, question)
		}

		// mode2: query/recall pipeline
		effort := parseEffort(askEffort)
		opts := &query.AskOptions{
			TopN:     askTopN,
			NoStream: askNoStream,
			Effort:   effort,
			Evidence: query.ParseEvidenceMode(askEvidence),
		}

		if askNoStream {
			return runAskNonStream(ctx, engine, question, opts)
		}
		return runAskStream(ctx, engine, question, opts)
	},
}

func init() {
	askCmd.Flags().BoolVar(&askNoStream, "no-stream", false, "Disable streaming output (wait for full answer)")
	askCmd.Flags().IntVarP(&askTopN, "top-n", "n", query.DefaultTopN, "Number of diverse search results to use as LLM context")
	askCmd.Flags().StringVar(&askEffort, "effort", "fast", "Search effort level: fast (no expansion), balanced (query expansion), thorough (HyDE)")
	askCmd.Flags().StringVar(&askEvidence, "evidence", "auto", "Evidence layer: auto (escalate when needed), raw (always attach sources), wiki (L1 only)")
	askCmd.Flags().BoolVar(&askAgent, "agent", false, "Use multi-step ReAct agent (wiki/raw/code tools; read-only)")
	askCmd.Flags().IntVar(&askMaxSteps, "max-steps", agent.DefaultMaxSteps, "Max ReAct steps when --agent is set")
	askCmd.Flags().StringArrayVar(&askAgentRoot, "agent-root", nil, "Extra filesystem root the agent may read (repeatable); wiki/raw always included")
}

// parseEffort converts a CLI effort string to a wiki.SearchEffort value.
// Unknown values default to SearchEffortFast.
func parseEffort(s string) wiki.SearchEffort {
	switch s {
	case "balanced":
		return wiki.SearchEffortBalanced
	case "thorough":
		return wiki.SearchEffortThorough
	default:
		return wiki.SearchEffortFast
	}
}

// Agent step display caps (bytes/runes via char count; UTF-8 safe enough for preview).
const (
	// Compact summary: max runes for action detail on one line (non-TTY).
	// Paths use suffix-preserving shorten (see truncateOneLine).
	agentCompactDetailMax = 96
	// Error expand (non-verbose): short observation preview.
	agentObsPreviewError = 800
	// Detailed non-verbose expand (unused for full dump; keep for final preview).
	agentObsPreviewDefault = 1500
	// -v: longer observation, plus decide prompts / raw LLM response.
	agentObsPreviewVerbose = 8000
	agentPromptPreview     = 6000
	agentLLMRawPreview     = 3000
)

// runAgent runs the embedded ReAct explorer via the query engine.
func runAgent(ctx context.Context, engine *query.Engine, question string) error {
	fmt.Printf("Agent exploring: %s\n\n", question)

	verbose := engine.Tracer().Enabled()
	opts := &agent.Options{
		MaxSteps:       askMaxSteps,
		WallTime:       120 * time.Second,
		Roots:          askAgentRoot,
		CollectPrompts: verbose, // attach system/user prompts + raw LLM when -v
	}

	// TTY default: progressive live view (docs/111). -v and non-TTY fall back
	// to plain text writers so pipelines and full transcripts stay simple.
	if !verbose && agentview.UseLive(os.Stderr) {
		view := agentview.NewDefault()
		defer view.Close()
		opts.OnProgress = view.OnProgress
		opts.OnStep = view.OnStep
	} else {
		opts.OnStep = func(s agent.Step) {
			writeAgentStep(os.Stderr, s, verbose)
		}
	}

	result, err := engine.AskAgent(ctx, question, opts)
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	fmt.Println(result.Answer)
	fmt.Println()
	printRefs(result.Refs, "References:", "?")
	if result.Truncated {
		fmt.Println("\n(note: agent stopped due to step/time budget)")
	}
	printAgentUsage(result)

	if promptSave() {
		if err := engine.SaveAnswer(question, result.Answer, result.Refs); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save: %v\n", err)
		} else {
			fmt.Println("Q&A saved to wiki synthesis page.")
		}
	}
	return nil
}

// writeAgentStep renders one ReAct turn for CLI observability (docs/111).
// Default: one-line compact summary. Verbose or error steps: multi-line detail.
func writeAgentStep(w io.Writer, s agent.Step, verbose bool) {
	if verbose || stepNeedsExpand(s) {
		writeAgentStepDetailed(w, s, verbose)
		return
	}
	writeAgentStepCompact(w, s)
}

// stepNeedsExpand reports whether a non-verbose step should still show detail
// (parse failures and tool/LLM errors) so problems are not hidden in a one-liner.
func stepNeedsExpand(s agent.Step) bool {
	if s.Thought == "parse_error" {
		return true
	}
	if strings.HasPrefix(s.Observation, "ERROR:") {
		return true
	}
	return false
}

// stepKind labels a step for detailed headers (stable vocabulary).
func stepKind(s agent.Step) string {
	switch {
	case s.Final:
		return "final_answer"
	case s.Thought == "parse_error":
		return "parse_error"
	case s.Action == "" && strings.HasPrefix(s.Observation, "ERROR:"):
		return "error"
	default:
		return "tool"
	}
}

// compactKind is the middle label on a one-line timeline row (tool name when known).
func compactKind(s agent.Step) string {
	switch {
	case s.Final:
		return "final_answer"
	case s.Thought == "parse_error":
		return "parse_error"
	case strings.HasPrefix(s.Observation, "ERROR:"):
		return "error"
	case s.Action != "":
		return s.Action
	default:
		return "tool"
	}
}

// stepStatusMark is the leading glyph for compact timeline rows.
func stepStatusMark(s agent.Step) string {
	if s.Thought == "parse_error" || strings.HasPrefix(s.Observation, "ERROR:") {
		return "✗"
	}
	if s.Final {
		return "→"
	}
	return "✓"
}

// writeAgentStepCompact prints a single skimmable line per successful step.
// Example: ✓ 3 · wiki_search · "GC" · 120ms · 48B · 100→20 tok
func writeAgentStepCompact(w io.Writer, s agent.Step) {
	parts := []string{
		fmt.Sprintf("%s %d", stepStatusMark(s), s.Index),
		compactKind(s),
	}
	if detail := formatActionDetail(s.Action, s.Args); detail != "" {
		parts = append(parts, truncateOneLine(detail, agentCompactDetailMax))
	} else if s.Final && s.Thought != "" && s.Thought != "parse_error" {
		parts = append(parts, truncateOneLine(s.Thought, agentCompactDetailMax))
	}
	parts = append(parts, s.Duration.Round(time.Millisecond).String())
	if size := formatObsSize(s); size != "" {
		parts = append(parts, size)
	}
	if tok := strings.TrimPrefix(formatStepTokens(s), ", "); tok != "" {
		parts = append(parts, tok)
	}
	fmt.Fprintln(w, strings.Join(parts, " · "))
}

// formatObsSize is a short observation bulk note for compact lines (e.g. "1.2kB").
func formatObsSize(s agent.Step) string {
	n := s.ObsBytes
	if n <= 0 {
		n = len(s.Observation)
	}
	if n <= 0 {
		return ""
	}
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fkB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

// truncateOneLine collapses whitespace and caps length for compact display.
// Path-like values keep the suffix (basename) so long absolute paths remain useful.
func truncateOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	// Keep end for paths (same idea as agentview.shortenDetail).
	if strings.Contains(s, "/") || strings.Contains(s, `\`) {
		if !strings.HasPrefix(s, `"`) {
			if len(s) > max-1 {
				return "…" + s[len(s)-(max-1):]
			}
		}
	}
	return s[:max-1] + "…"
}

// writeAgentStepDetailed prints Thought → Action → Observation (and with
// verbose: decide prompt + raw LLM response). Used for -v and auto-expanded errors.
func writeAgentStepDetailed(w io.Writer, s agent.Step, verbose bool) {
	tok := formatStepTokens(s)
	fmt.Fprintf(w, "── Step %d · %s (%s%s) ──\n",
		s.Index, stepKind(s), s.Duration.Round(time.Millisecond), tok)

	// Verbose: show the decide-round prompt material first (what the model saw).
	if verbose {
		writeAgentPromptDetail(w, s)
	}

	// Thought (model reasoning)
	if s.Thought != "" && s.Thought != "parse_error" {
		fmt.Fprintf(w, "Thought:\n")
		writeIndentedBlock(w, s.Thought, "  ")
	} else if s.Thought == "parse_error" {
		fmt.Fprintf(w, "Thought: (model returned unparseable JSON)\n")
		if s.ParseDumpPath != "" {
			fmt.Fprintf(w, "  dump: %s\n", s.ParseDumpPath)
		}
	}

	if s.Final {
		if s.FinalAnswer != "" {
			preview := s.FinalAnswer
			limit := agentObsPreviewDefault
			if verbose {
				limit = agentObsPreviewVerbose
			}
			previewNote := ""
			if len(preview) > limit {
				preview = preview[:limit]
				previewNote = " (preview)"
			}
			fmt.Fprintf(w, "Final answer%s (%d chars):\n", previewNote, len(s.FinalAnswer))
			writeIndentedBlock(w, preview, "  ")
			if previewNote != "" {
				fmt.Fprintf(w, "  …\n")
			}
		} else {
			fmt.Fprintf(w, "Final answer ready.\n")
		}
		fmt.Fprintln(w)
		return
	}

	// Action + args
	if s.Action != "" {
		fmt.Fprintf(w, "Action: %s\n", s.Action)
		if verbose && len(s.Args) > 0 {
			if argsJSON, err := json.MarshalIndent(s.Args, "  ", "  "); err == nil {
				fmt.Fprintf(w, "  args: %s\n", string(argsJSON))
			} else {
				fmt.Fprintf(w, "  args: %v\n", s.Args)
			}
		}
		if detail := formatActionDetail(s.Action, s.Args); detail != "" {
			fmt.Fprintf(w, "  summary: %s\n", detail)
		}
	}

	// Observation (tool result)
	if s.Observation != "" {
		limit := agentObsPreviewError
		if verbose {
			limit = agentObsPreviewVerbose
		}
		obs := s.Observation
		shownTruncated := false
		if len(obs) > limit {
			obs = obs[:limit]
			shownTruncated = true
		}
		sizeNote := ""
		if s.ObsBytes > 0 {
			sizeNote = fmt.Sprintf(" (%d bytes", s.ObsBytes)
			if shownTruncated || len(s.Observation) < s.ObsBytes {
				sizeNote += ", preview"
			}
			sizeNote += ")"
		} else if shownTruncated {
			sizeNote = " (preview)"
		}
		label := "Observation"
		if strings.HasPrefix(s.Observation, "ERROR:") {
			label = "Observation · ERROR"
		}
		fmt.Fprintf(w, "%s%s:\n", label, sizeNote)
		writeIndentedBlock(w, obs, "  ")
		if shownTruncated || (s.ObsBytes > 0 && len(s.Observation) < s.ObsBytes) {
			fmt.Fprintf(w, "  …\n")
		}
	}

	fmt.Fprintln(w)
}

// writeAgentPromptDetail prints decide-round prompts and raw LLM output (-v only).
func writeAgentPromptDetail(w io.Writer, s agent.Step) {
	if s.SystemPrompt == "" && s.UserPrompt == "" && s.LLMRaw == "" {
		return
	}
	fmt.Fprintf(w, "Decide prompt:\n")
	if s.SystemPrompt != "" {
		fmt.Fprintf(w, "  [system] (%d chars)\n", len(s.SystemPrompt))
		writeIndentedBlock(w, truncateForDisplay(s.SystemPrompt, agentPromptPreview), "    ")
		if len(s.SystemPrompt) > agentPromptPreview {
			fmt.Fprintf(w, "    …\n")
		}
	}
	if s.UserPrompt != "" {
		fmt.Fprintf(w, "  [user] (%d chars)\n", len(s.UserPrompt))
		writeIndentedBlock(w, truncateForDisplay(s.UserPrompt, agentPromptPreview), "    ")
		if len(s.UserPrompt) > agentPromptPreview {
			fmt.Fprintf(w, "    …\n")
		}
	}
	if s.LLMRaw != "" {
		fmt.Fprintf(w, "LLM response (%d chars):\n", len(s.LLMRaw))
		writeIndentedBlock(w, truncateForDisplay(s.LLMRaw, agentLLMRawPreview), "  ")
		if len(s.LLMRaw) > agentLLMRawPreview {
			fmt.Fprintf(w, "  …\n")
		}
	}
}

// writeIndentedBlock writes each line of text with a fixed prefix.
func writeIndentedBlock(w io.Writer, text, prefix string) {
	if text == "" {
		return
	}
	// Avoid strings.Split allocating for trailing newline-only differences.
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for _, line := range lines {
		fmt.Fprintf(w, "%s%s\n", prefix, line)
	}
}

// truncateForDisplay cuts s to n runes-ish (bytes) with no ellipsis (caller adds it).
func truncateForDisplay(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

// runAskNonStream performs a blocking ask and prints the full answer at once.
func runAskNonStream(ctx context.Context, engine *query.Engine, question string, opts *query.AskOptions) error {
	fmt.Printf("Asking: %s\n", question)
	fmt.Println("Thinking...")

	result, err := engine.Ask(ctx, question, opts)
	if err != nil {
		return fmt.Errorf("ask failed: %w", err)
	}

	fmt.Println()
	fmt.Println(result.Answer)
	fmt.Println()

	printRefs(result.Refs, "References:", "wiki")

	if promptSave() {
		if err := engine.SaveAnswer(question, result.Answer, result.Refs); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save: %v\n", err)
		} else {
			fmt.Println("Q&A saved to wiki synthesis page.")
		}
	}

	return nil
}

// runAskStream performs a streaming ask, printing chunks as they arrive.
func runAskStream(ctx context.Context, engine *query.Engine, question string, opts *query.AskOptions) error {
	fmt.Printf("Asking: %s\n\n", question)

	ch, err := engine.AskStream(ctx, question, opts)
	if err != nil {
		return fmt.Errorf("ask stream failed: %w", err)
	}

	var refs []wiki.Ref
	var fullAnswer strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			return fmt.Errorf("stream error: %w", chunk.Error)
		}
		if chunk.Done {
			refs = chunk.Refs
			break
		}
		fmt.Print(chunk.Content)
		fullAnswer.WriteString(chunk.Content)
	}
	fmt.Println()

	printRefs(refs, "References:", "wiki")

	if promptSave() {
		if err := engine.SaveAnswer(question, fullAnswer.String(), refs); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save: %v\n", err)
		} else {
			fmt.Println("Q&A saved to wiki synthesis page.")
		}
	}

	return nil
}

// formatActionDetail extracts key arguments from a tool call for human-readable
// verbose output (e.g. search query, file path, grep pattern).
func formatActionDetail(action string, args map[string]any) string {
	switch action {
	case "wiki_search", "raw_search":
		q := strArg(args, "query")
		n := intArg(args, "top_n")
		if n > 0 {
			return fmt.Sprintf("%q (top_n=%d)", q, n)
		}
		return fmt.Sprintf("%q", q)
	case "wiki_index":
		if f := strArg(args, "filter"); f != "" {
			return fmt.Sprintf("filter=%q", f)
		}
		return "all"
	case "wiki_read", "raw_read", "file_read":
		return strArg(args, "path")
	case "wiki_links":
		return strArg(args, "path")
	case "raw_list_sources":
		if p := strArg(args, "path"); p != "" {
			return p
		}
		return "all"
	case "file_grep":
		pattern := strArg(args, "pattern")
		glob := strArg(args, "glob")
		if glob != "" {
			return fmt.Sprintf("%q (glob=%s)", pattern, glob)
		}
		return fmt.Sprintf("%q", pattern)
	case "symbol_search":
		return strArg(args, "name")
	case "list_dir", "ast_outline":
		return strArg(args, "path")
	case "read_enclosing":
		path := strArg(args, "path")
		line := intArg(args, "line")
		if line > 0 {
			return fmt.Sprintf("%s:%d", path, line)
		}
		return path
	default:
		return ""
	}
}

// formatStepTokens returns a ", prompt→completion tok" suffix for step logs.
// Falls back to estimated prompt tokens from PromptChars when usage is missing.
func formatStepTokens(s agent.Step) string {
	if s.PromptTokens > 0 || s.CompletionTokens > 0 {
		return fmt.Sprintf(", %d→%d tok", s.PromptTokens, s.CompletionTokens)
	}
	if s.PromptChars > 0 {
		// ~3 chars/token for mixed CJK+Latin (conservative display estimate)
		est := s.PromptChars / 3
		return fmt.Sprintf(", ~%dk chars (~%d tok est)", s.PromptChars/1000, est)
	}
	return ""
}

// printAgentUsage prints cumulative LLM usage after an agent run.
func printAgentUsage(result *agent.Result) {
	if result == nil || len(result.Steps) == 0 {
		return
	}
	if result.TotalPromptTokens > 0 || result.TotalCompletionTokens > 0 {
		fmt.Fprintf(os.Stderr, "\nToken usage: %d prompt + %d completion = %d total across %d steps\n",
			result.TotalPromptTokens, result.TotalCompletionTokens,
			result.TotalPromptTokens+result.TotalCompletionTokens, len(result.Steps))
		return
	}
	if result.TotalPromptChars > 0 {
		est := result.TotalPromptChars / 3
		fmt.Fprintf(os.Stderr, "\nContext size: %d prompt chars (~%d tok est) across %d steps (provider did not report token usage)\n",
			result.TotalPromptChars, est, len(result.Steps))
	}
}

// strArg extracts a string value from tool args, defaulting to "".
func strArg(args map[string]any, key string) string {
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

// intArg extracts an int value from tool args (accepts float64 from JSON), defaulting to 0.
func intArg(args map[string]any, key string) int {
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

// printRefs prints reference blocks with a header and a default layer
// for refs whose layer is empty.
func printRefs(refs []wiki.Ref, header, defaultLayer string) {
	if len(refs) == 0 {
		return
	}
	fmt.Println("---")
	fmt.Println(header)
	for _, src := range refs {
		layer := src.Layer
		if layer == "" {
			layer = defaultLayer
		}
		fmt.Printf("  - [%s] %s (%s)\n", layer, src.Title, src.Path)
	}
}

// promptSave asks the user whether to save the answer as a wiki page.
// It returns false in non-interactive (pipe) mode.
func promptSave() bool {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return false // non-interactive (pipe), skip prompt
	}

	fmt.Fprint(os.Stderr, "\nSave this answer to wiki? [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}
