package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/query"
)

var (
	askSave     bool
	askNoStream bool
	askTopN     int
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question and get AI-synthesized answer from wiki",
	Long: `Search relevant wiki pages and use LLM to synthesize
	a comprehensive answer with citations.

The ask pipeline:
  1. Search wiki pages using FTS5 full-text search
  2. Retrieve top-N most relevant page contents
  3. Build LLM prompt with context pages + question
  4. Stream the synthesized answer (or output all at once with --no-stream)
  5. Answer includes citations in [[page]] notation

Examples:
  ruminate ask "What is RAG?"
  ruminate ask --save "How does FTS5 work?"
  ruminate ask --top-n 10 "Explain the attention mechanism"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question := args[0]

		// Load configuration
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Create query engine (internally initializes wiki.Manager)
		engine, err := query.NewEngine(cfg)
		if err != nil {
			return err
		}

		opts := &query.AskOptions{
			TopN:     askTopN,
			Save:     askSave,
			NoStream: askNoStream,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle Ctrl-C gracefully
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			<-sigCh
			cancel()
		}()

		if askNoStream {
			return runAskNonStream(ctx, engine, question, opts)
		}
		return runAskStream(ctx, engine, question, opts)
	},
}

func init() {
	askCmd.Flags().BoolVar(&askSave, "save", false, "Save the Q&A result as a wiki synthesis page")
	askCmd.Flags().BoolVar(&askNoStream, "no-stream", false, "Disable streaming output (wait for full answer)")
	askCmd.Flags().IntVarP(&askTopN, "top-n", "n", 5, "Number of top search results to use as context")
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
			fmt.Printf("  - %s (%s)\n", src.Title, src.Path)
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
			fmt.Printf("  - %s (%s)\n", src.Title, src.Path)
		}
	}

	if opts.Save {
		fmt.Println("\nQ&A saved to wiki synthesis page.")
	}

	return nil
}
