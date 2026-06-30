package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest <file|url>",
	Short: "Ingest source material into the wiki",
	Long: `Read source material (Markdown, plain text, URL) and use LLM
to analyze, extract key information, and update wiki pages.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]
		fmt.Printf("Ingesting: %s\n", source)
		fmt.Println("(ingest engine not yet implemented)")
		return nil
	},
}
