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
	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/trace"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var (
	askSave      bool
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
a comprehensive answer with citations.

The ask pipeline (default):
  1. Search wiki pages using hybrid/FTS retrieval (L1 Synthesis)
  2. Auto-escalate to raw Evidence when needed (L2; configurable via --evidence)
  3. Build LLM prompt with context + question
  4. Stream the synthesized answer (or --no-stream)
  5. Answer includes citations in [[page]] notation

Agent mode (--agent): multi-step ReAct exploration (docs/109).
  Uses tools: wiki_*, raw_*, file_grep/read, symbol_search, read_enclosing.
  Default read-only; code intelligence is syntactic (go/ast), not gopls.

Examples:
  ruminate ask "What is RAG?"
  ruminate ask --evidence auto "原文默认超时是多少？"
  ruminate ask --agent "Reconcile 会不会阻塞？"
  ruminate ask --agent --agent-root /path/to/code "Where is Hello defined?"
  ruminate ask --save "How does FTS5 work?"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question := strings.Join(args, " ")

		// Load configuration
		wikiName, _ := cmd.Flags().GetString("wiki")
		cfg, err := loadRuntimeConfig(wikiName)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		tr := trace.New(verbose)
		defer tr.Flush(os.Stderr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle Ctrl-C gracefully
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			<-sigCh
			cancel()
		}()

		if askAgent {
			return runAgent(ctx, cfg, question, tr)
		}

		// Create query engine (internally initializes wiki.Manager)
		engine, err := query.NewEngine(cfg)
		if err != nil {
			return err
		}
		engine.SetTracer(tr)

		effort := parseEffort(askEffort)
		opts := &query.AskOptions{
			TopN:     askTopN,
			Save:     askSave,
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
	askCmd.Flags().BoolVar(&askSave, "save", false, "Save the Q&A result as a wiki synthesis page")
	askCmd.Flags().BoolVar(&askNoStream, "no-stream", false, "Disable streaming output (wait for full answer)")
	askCmd.Flags().IntVarP(&askTopN, "top-n", "n", query.DefaultTopN, "Number of diverse search results to use as LLM context")
	askCmd.Flags().StringVar(&askEffort, "effort", "fast", "Search effort level: fast (no expansion), balanced (query expansion), thorough (HyDE)")
	askCmd.Flags().StringVar(&askEvidence, "evidence", "auto", "Evidence layer: auto (escalate when needed), raw (always attach sources), wiki (L1 only)")
	askCmd.Flags().BoolVar(&askAgent, "agent", false, "Use multi-step ReAct agent (wiki/raw/code tools; read-only)")
	askCmd.Flags().IntVar(&askMaxSteps, "max-steps", 12, "Max ReAct steps when --agent is set")
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

// runAgent runs the embedded ReAct explorer.
func runAgent(ctx context.Context, cfg *config.RuntimeConfig, question string, tr *trace.Tracer) error {
	mgr, err := wiki.NewManagerFromConfig(cfg.WikiPath, cfg.LLM, cfg.Embedding)
	if err != nil {
		return err
	}
	defer mgr.Close()
	if !mgr.IsInitialized() {
		return fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
	}

	provider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKey)
	if err != nil {
		return fmt.Errorf("LLM provider: %w", err)
	}

	ex := agent.NewExplorer(mgr, provider, cfg.LLM)
	ex.SetTracer(tr)

	roots := []string{mgr.WikiDir(), mgr.RawDir(), mgr.Root()}
	roots = append(roots, askAgentRoot...)

	fmt.Printf("Agent exploring: %s\n\n", question)

	verbose := tr.Enabled()

	opts := &agent.Options{
		MaxSteps: askMaxSteps,
		WallTime: 120 * time.Second,
		Roots:    roots,
		Save:     askSave,
		OnStep: func(s agent.Step) {
			if s.Final {
				fmt.Fprintf(os.Stderr, "  [step %d] final_answer\n", s.Index)
				return
			}
			if verbose {
				detail := formatActionDetail(s.Action, s.Args)
				if detail != "" {
					fmt.Fprintf(os.Stderr, "  [step %d] %s %s (%s)\n", s.Index, s.Action, detail, s.Duration.Round(time.Millisecond))
					return
				}
			}
			fmt.Fprintf(os.Stderr, "  [step %d] %s (%s)\n", s.Index, s.Action, s.Duration.Round(time.Millisecond))
		},
	}

	result, err := ex.Run(ctx, question, opts)
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	fmt.Println(result.Answer)
	fmt.Println()

	if len(result.Citations) > 0 {
		fmt.Println("---")
		fmt.Println("Citations:")
		for _, c := range result.Citations {
			layer := c.Layer
			if layer == "" {
				layer = "?"
			}
			fmt.Printf("  - [%s] %s (%s)\n", layer, c.Title, c.Path)
		}
	}
	if result.Truncated {
		fmt.Println("\n(note: agent stopped due to step/time budget)")
	}
	if askSave {
		fmt.Println("\nQ&A saved to wiki synthesis page.")
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

	if len(result.Sources) > 0 {
		fmt.Println("---")
		fmt.Println("Sources:")
		for _, src := range result.Sources {
			layer := src.Layer
			if layer == "" {
				layer = "wiki"
			}
			fmt.Printf("  - [%s] %s (%s)\n", layer, src.Title, src.Path)
		}
	}

	if opts.Save {
		fmt.Println("\nQ&A saved to wiki synthesis page.")
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

	var sources []query.Source
	for chunk := range ch {
		if chunk.Error != nil {
			return fmt.Errorf("stream error: %w", chunk.Error)
		}
		if chunk.Done {
			sources = chunk.Sources
			break
		}
		fmt.Print(chunk.Content)
	}
	fmt.Println()

	if len(sources) > 0 {
		fmt.Println("\nSources:")
		for _, src := range sources {
			layer := src.Layer
			if layer == "" {
				layer = "wiki"
			}
			fmt.Printf("  - [%s] %s (%s)\n", layer, src.Title, src.Path)
		}
	}

	if opts.Save {
		fmt.Println("\nQ&A saved to wiki synthesis page.")
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
