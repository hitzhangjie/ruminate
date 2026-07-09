package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ruminate",
	Short: "Ruminate - AI-driven personal knowledge base",
	Long: `Ruminate is an AI-driven personal knowledge base system.
It incrementally builds and maintains a persistent, interlinked Markdown wiki —
curated by you, written and maintained entirely by LLMs.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose pipeline logging to stderr")
	rootCmd.PersistentFlags().String("wiki", "", "Target wiki name (uses default if omitted)")

	rootCmd.AddCommand(ingestCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(findCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(hookCmd)
}

// exitWithError prints an error message and exits with code 1.
func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
