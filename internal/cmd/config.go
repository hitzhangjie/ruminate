package cmd

import (
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or validate configuration",
	Long: `Display the current configuration or validate a config file.
Without subcommands, prints the effective configuration in YAML format.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		printConfig(cfg)
		return nil
	},
}
