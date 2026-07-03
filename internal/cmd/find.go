package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var (
	findLimit        int
	snippetFormatter func(string) string
)

var findCmd = &cobra.Command{
	Use:   "find <keywords>",
	Short: "Full-text search across wiki pages",
	Long: `Search wiki pages and raw sources using SQLite FTS5 full-text search.

Returns matching pages ranked by BM25 relevance with highlighted snippets.
Matching terms in snippets are displayed in bold.

Examples:
  ruminate find "machine learning"
  ruminate find --limit 5 "kubernetes"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keywords := args[0]

		// Load configuration
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Open wiki manager (search only needs index, not LLM)
		mgr := wiki.NewManager(cfg.WikiPath)
		if !mgr.IsInitialized() {
			return fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
		}

		// Search with snippets
		results, err := mgr.Index().SearchWithSnippets(keywords, findLimit)
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}

		if len(results) == 0 {
			fmt.Println("No results found.")
			return nil
		}

		// Print results
		fmt.Printf("Found %d result(s) for \"%s\":\n\n", len(results), keywords)
		for i, r := range results {
			fmt.Printf("--- %d. %s", i+1, r.Title)
			if r.Type != "" && r.Type != "raw" {
				fmt.Printf(" (%s)", r.Type)
			}
			fmt.Printf(" [%s] ---\n", r.Path)
			snippet := snippetFormatter(r.Snippet)
			fmt.Printf("   %s\n\n", snippet)
		}

		return nil
	},
}

func init() {
	findCmd.Flags().IntVarP(&findLimit, "limit", "n", 20, "Maximum number of results")

	// Check if stdout is a terminal; use ANSI bold for terminals, plain text otherwise
	if fi, _ := os.Stdout.Stat(); fi != nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		snippetFormatter = ansiHighlight
	} else {
		snippetFormatter = plainHighlight
	}
}

// ansiHighlight converts FTS5 <b> tags to ANSI bold escape sequences.
func ansiHighlight(s string) string {
	s = strings.ReplaceAll(s, "<b>", "\033[1m")
	s = strings.ReplaceAll(s, "</b>", "\033[0m")
	return s
}

// plainHighlight strips <b> tags without ANSI codes (for non-terminal output).
func plainHighlight(s string) string {
	s = strings.ReplaceAll(s, "<b>", "")
	s = strings.ReplaceAll(s, "</b>", "")
	return s
}
