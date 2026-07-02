package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/ingest"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest <file|url>",
	Short: "Ingest source material into the wiki",
	Long: `Read source material (Markdown, plain text, or URL) and use LLM
to analyze, extract key information, and update wiki pages.

The ingestion pipeline:
  1. Read the source file or URL
  2. Save a raw copy to raw/<source-type>/
  3. Analyze with LLM to extract summary, entities, concepts, and key points
  4. Create or update wiki pages with cross-references
  5. Commit all changes to git

Source type (-t) classifies the material by its form and depth, not by
file format or publishing channel. This determines how raw sources are
organized under raw/<source-type>/ and how they are displayed in wiki pages.

Available types:
  article   Long-form written content (articles, blog posts, tutorials, news,
            essays). The publishing channel doesn't matter — a blog post and
            a magazine article serve the same role.
  paper     Academic or research papers with formal methodology, citations,
            and structured arguments.
  note      Short, informal, or fragmentary content: quick notes, chat logs,
            voice transcriptions, temporary jottings.
  book      Book excerpts, reading notes, or book reviews.

Defaults to "note" when not specified. This is intentionally neutral —
run "ruminate ingest" again with -t to re-classify if needed.

Requires an initialized wiki (run "ruminate init" first).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]
		sourceType, _ := cmd.Flags().GetString("type")

		// Load configuration
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Open wiki manager
		mgr := wiki.NewManager(cfg.WikiPath)
		if !mgr.IsInitialized() {
			return fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
		}

		// Create LLM provider
		provider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model)
		if err != nil {
			return fmt.Errorf("creating LLM provider: %w", err)
		}

		// Create ingest engine
		engine := ingest.NewEngine(mgr, provider, cfg.LLM)

		// Run ingestion
		fmt.Printf("Ingesting: %s (type: %s)\n", source, sourceType)
		fmt.Println("  Analyzing with LLM...")

		ctx := context.Background()
		if err := engine.Ingest(ctx, source, sourceType); err != nil {
			return fmt.Errorf("ingest failed: %w", err)
		}

		fmt.Println("Ingest completed successfully.")
		fmt.Println("  - Summary page created/updated")
		fmt.Println("  - Entity pages created/updated")
		fmt.Println("  - Concept pages created/updated")
		fmt.Println("  - Changes committed to git")
		return nil
	},
}

func init() {
	ingestCmd.Flags().StringP("type", "t", "note", "Source type: article, paper, note, book")
	rootCmd.AddCommand(ingestCmd)
}
