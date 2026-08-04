package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/agent"
	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/trace"
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

Agent mode (--agent): multi-step ReAct exploration (docs/109).
  Uses tools: wiki_*, raw_*, file_grep/read, symbol_search, read_enclosing.
  Default read-only; code intelligence is syntactic (go/ast), not gopls.

Examples:
  ruminate ask "What is RAG?"
  ruminate ask --evidence auto "原文默认超时是多少？"
  ruminate ask --agent "Reconcile 会不会阻塞？"
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

// runAgent runs the embedded ReAct explorer via the query engine.
func runAgent(ctx context.Context, engine *query.Engine, question string) error {
	fmt.Printf("Agent exploring: %s\n\n", question)

	verbose := engine.Tracer().Enabled()
	opts := &agent.Options{
		MaxSteps: askMaxSteps,
		WallTime: 120 * time.Second,
		Roots:    askAgentRoot,
		OnStep: func(s agent.Step) {
			tok := formatStepTokens(s)
			if s.Final {
				fmt.Fprintf(os.Stderr, "  [step %d] final_answer (%s%s)\n",
					s.Index, s.Duration.Round(time.Millisecond), tok)
				return
			}
			if s.Thought == "parse_error" {
				if s.ParseDumpPath != "" {
					fmt.Fprintf(os.Stderr, "  [step %d] parse_error (%s%s) → dumped to %s\n",
						s.Index, s.Duration.Round(time.Millisecond), tok, s.ParseDumpPath)
				} else {
					fmt.Fprintf(os.Stderr, "  [step %d] parse_error (%s%s)\n",
						s.Index, s.Duration.Round(time.Millisecond), tok)
				}
				return
			}
			if verbose {
				detail := formatActionDetail(s.Action, s.Args)
				if detail != "" {
					fmt.Fprintf(os.Stderr, "  [step %d] %s %s (%s%s)\n",
						s.Index, s.Action, detail, s.Duration.Round(time.Millisecond), tok)
					return
				}
			}
			fmt.Fprintf(os.Stderr, "  [step %d] %s (%s%s)\n",
				s.Index, s.Action, s.Duration.Round(time.Millisecond), tok)
		},
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
