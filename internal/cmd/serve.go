package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server and serve the web UI",
	Long: `Start the backend API server and proxy the frontend dev server.
In production mode, serves the embedded static files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Starting server...")
		fmt.Println("(HTTP server not yet implemented)")
		return nil
	},
}
