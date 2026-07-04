package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild FTS index with CJK bigram support",
	Long: `Rebuild the full-text search index for all wiki pages and raw sources
with CJK bigram tokenization. This enables sub-phrase matching for Chinese,
Japanese, and Korean text queries.

Run this after upgrading Ruminate to enable CJK-aware search on existing content.

Examples:
  ruminate reindex`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		mgr := wiki.NewManager(cfg)
		if !mgr.IsInitialized() {
			return fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
		}
		defer mgr.Close()

		fmt.Println("Rebuilding FTS index with CJK bigram support...")
		if err := mgr.Reindex(); err != nil {
			return fmt.Errorf("reindex failed: %w", err)
		}

		fmt.Println("Reindex complete.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reindexCmd)
}
