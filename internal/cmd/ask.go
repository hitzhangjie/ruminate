package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question and get AI-synthesized answer from wiki",
	Long: `Search relevant wiki pages and use LLM to synthesize
a comprehensive answer with citations.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question := args[0]
		fmt.Printf("Asking: %s\n", question)
		fmt.Println("(query engine not yet implemented)")
		return nil
	},
}
