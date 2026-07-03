package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var initCmd = &cobra.Command{
	Use:   "init [wiki-path]",
	Short: "Initialize a new wiki directory",
	Long: `Initialize a new Ruminate wiki directory with the standard structure:

  <wiki_root>/
  ├── raw/          # Source materials (immutable)
  ├── wiki/         # Generated wiki pages
  ├── index.md      # Human-readable index
  ├── log.md        # Operations log
  ├── schema.md     # Wiki structure definition
  └── .ruminate/    # Internal state (FTS index, etc.)

If no path is given, uses the wiki_path from configuration (default: "ruminate_wiki").`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wikiPath := "~/ruminate_wiki"
		if len(args) > 0 {
			wikiPath = args[0]
		}

		mgr := wiki.NewManager(wikiPath)
		if mgr.IsInitialized() {
			fmt.Printf("Wiki already initialized at: %s\n", wikiPath)
			return nil
		}

		if err := mgr.Init(); err != nil {
			return fmt.Errorf("initializing wiki: %w", err)
		}

		fmt.Printf("Wiki initialized at: %s\n", wikiPath)
		fmt.Println()
		fmt.Println("Directory structure created:")
		fmt.Println("  raw/          — source materials (organized by user-defined content type)")
		fmt.Println("  wiki/         — generated wiki pages (summaries, entities, concepts, synthesis)")
		fmt.Println("  index.md      — human-readable page index")
		fmt.Println("  log.md        — operations log")
		fmt.Println("  schema.md     — wiki structure and conventions")
		fmt.Println("  .ruminate/    — internal state (FTS5 index)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
