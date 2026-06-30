package cmd

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/hitzhangjie/ruminate/internal/config"
)

// loadConfig loads the effective configuration from disk.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg, nil
}

// printConfig prints the configuration in YAML format.
func printConfig(cfg *config.Config) {
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding config: %v\n", err)
	}
}
