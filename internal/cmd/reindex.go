package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild the full-text search index",
	Long: `Rebuild the full-text search index for all wiki pages and raw sources.

This is useful when the index is out of sync or corrupted, or after upgrading
from a version that used a different tokenization scheme.

Examples:
  ruminate reindex`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wikiName, _ := cmd.Flags().GetString("wiki")
		cfg, err := loadRuntimeConfig(wikiName)
		if err != nil {
			return err
		}

		mgr, err := wiki.NewManagerFromConfig(cfg.WikiPath, cfg.LLM, cfg.Embedding)
		if err != nil {
			return err
		}
		if !mgr.IsInitialized() {
			return fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
		}
		defer mgr.Close()

		fmt.Println("Rebuilding FTS index...")
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
