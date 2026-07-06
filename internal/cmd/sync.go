package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/sync"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync changes from a source repository into the knowledge base",
	Long: `Detect file changes (adds, modifications, renames, deletions) in a source
repository and incrementally ingest them into the ruminate knowledge base.

The sync command compares the current HEAD of the source repo against the
last synced commit (stored in the wiki's .ruminate/sync_state.json) and
processes each changed file:

  A (add)     → ingest the new file
  M (modify)  → re-ingest to update summary/entity/concept pages
  R (rename)  → ingest the renamed file (LLM dedup handles merging)
  D (delete)  → log the deletion; wiki content is preserved with a warning

On first run (no previous sync state), all tracked files in the source repo
are ingested.

ruminate sync can be used standalone or triggered automatically by a git
post-commit hook (see "ruminate hook install").

Requires an initialized wiki (run "ruminate init" first).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, _ := cmd.Flags().GetString("repo")
		sourceType, _ := cmd.Flags().GetString("source-type")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		// Default repo to current directory
		if repo == "" {
			var err error
			repo, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
		}

		// Expand path
		repo = config.ExpandPath(repo)

		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		engine, err := sync.NewEngine(cfg, repo, sourceType, dryRun)
		if err != nil {
			return err
		}

		ctx := context.Background()
		result, err := engine.Sync(ctx)
		if err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}

		// Print summary
		fmt.Println()
		fmt.Println("Sync summary:")
		fmt.Printf("  Source repo: %s\n", result.SourceRepo)
		if dryRun {
			fmt.Println("  Mode: dry-run (no changes applied)")
		}
		fmt.Printf("  Added:    %d\n", result.FilesAdded)
		fmt.Printf("  Modified: %d\n", result.FilesModified)
		fmt.Printf("  Renamed:  %d\n", result.FilesRenamed)
		fmt.Printf("  Deleted:  %d (wiki content preserved)\n", result.FilesDeleted)

		if len(result.Errors) > 0 {
			fmt.Printf("\n  Errors: %d\n", len(result.Errors))
			for _, e := range result.Errors {
				fmt.Printf("    - %s\n", e)
			}
		}

		return nil
	},
}

func init() {
	syncCmd.Flags().StringP("repo", "r", "", "Path to source repository (defaults to current directory)")
	syncCmd.Flags().StringP("source-type", "t", "note", "Default source type for synced files: article, paper, note, book")
	syncCmd.Flags().Bool("dry-run", false, "Detect changes without applying them")
}
