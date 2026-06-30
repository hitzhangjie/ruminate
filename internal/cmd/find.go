package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var findCmd = &cobra.Command{
	Use:   "find <keywords>",
	Short: "Full-text search across wiki pages",
	Long: `Search wiki pages using SQLite FTS5 full-text search.
Returns matching pages with highlighted snippets.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keywords := args[0]
		fmt.Printf("Finding: %s\n", keywords)
		fmt.Println("(search engine not yet implemented)")
		return nil
	},
}
