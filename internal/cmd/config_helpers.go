package cmd

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/hitzhangjie/ruminate/internal/config"
)

// loadRuntimeConfig resolves the target wiki and returns a combined RuntimeConfig.
// wikiName is the value of the --wiki flag (empty string if not specified).
func loadRuntimeConfig(wikiName string) (*config.RuntimeConfig, error) {
	if wikiName == "" {
		var err error
		wikiName, err = config.ResolveDefaultWikiName()
		if err != nil {
			return nil, fmt.Errorf("resolving default wiki: %w", err)
		}
	}
	rt, err := config.ResolveRuntimeConfig(wikiName)
	if err != nil {
		return nil, fmt.Errorf("resolving wiki: %w", err)
	}
	return rt, nil
}

// print prints an arbitrary value as YAML.
func print(v any) {
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding config: %v\n", err)
	}
}
