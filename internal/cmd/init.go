package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var initCmd = &cobra.Command{
	Use:   "init <wikiname>",
	Short: "Initialize a new wiki",
	Long: `Initialize a new Ruminate wiki under $HOME/.ruminate/<wikiname>.

Creates the standard wiki directory structure:

  $HOME/.ruminate/<name>/
  ├── raw/          # Source materials (immutable)
  ├── wiki/         # Generated wiki pages
  ├── index.md      # Human-readable index
  ├── log.md        # Operations log
  ├── schema.md     # Wiki structure definition
  └── db/            # Internal state (FTS index, etc.)

Also registers the wiki in $HOME/.ruminate/config.yaml.
If this is the first wiki, it is automatically set as the default.

Use --llm and --embedding to specify a named provider to use for this wiki.
If not specified, the wiki inherits the default providers. Provider overrides
are stored in the wiki's entry in the config file.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// Validate wiki name.
		if err := config.ValidateWikiName(name); err != nil {
			return err
		}

		// Check if wiki already exists.
		if config.WikiExists(name) {
			return fmt.Errorf("wiki %q already exists at %s", name,
				config.WikiEntry{Name: name}.Path())
		}

		// Load config to validate --llm / --embedding flags.
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		llmFlag, _ := cmd.Flags().GetString("llm")
		embeddingFlag, _ := cmd.Flags().GetString("embedding")

		// Validate provider references against the provider registry.
		if llmFlag != "" {
			if err := config.ValidateLLMProvider(cfg, llmFlag); err != nil {
				return err
			}
		}
		if embeddingFlag != "" {
			if err := config.ValidateEmbeddingProvider(cfg, embeddingFlag); err != nil {
				return err
			}
		}

		// Create wiki directory structure. Init() only creates directories
		// and bootstraps index/log/schema — it does not need LLM/embedding
		// providers, so we pass empty configs.
		wikiPath := config.WikiEntry{Name: name}.Path()
		mgr, err := wiki.NewManagerFromConfig(wikiPath, config.LLMConfig{}, config.EmbeddingConfig{})
		if err != nil {
			return fmt.Errorf("creating wiki manager: %w", err)
		}
		if err := mgr.Init(); err != nil {
			return fmt.Errorf("initializing wiki: %w", err)
		}

		// Register wiki with optional provider overrides.
		entry := config.WikiEntry{
			Name:      name,
			LLM:       llmFlag,
			Embedding: embeddingFlag,
		}
		cfg.Wikis = append(cfg.Wikis, entry)

		// Auto-set as default if this is the only wiki.
		isFirst := len(cfg.Wikis) == 1
		if isFirst {
			cfg.DefaultWiki = name
		}

		// Save config.
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		// Print summary.
		fmt.Printf("Wiki %q initialized at %s\n", name, wikiPath)
		if isFirst {
			fmt.Println("Set as default wiki (only wiki so far).")
		} else {
			fmt.Printf("Use \"ruminate config set default-wiki %s\" to switch default wiki.\n", name)
		}
		fmt.Println()
		fmt.Println("Directory structure created:")
		fmt.Println("  raw/          — source materials")
		fmt.Println("  wiki/         — generated wiki pages")
		fmt.Println("  db/            — internal state (FTS5 index, sync state)")
		fmt.Println("  index.md      — human-readable page index")
		fmt.Println("  log.md        — operations log")
		fmt.Println("  schema.md     — wiki structure and conventions")
		fmt.Println()
		providerInfo := "default providers"
		if llmFlag != "" || embeddingFlag != "" {
			providerInfo = "provider overrides saved to config"
		}
		fmt.Printf("Using %s.\n", providerInfo)
		if llmFlag != "" {
			fmt.Printf("  LLM: %s\n", llmFlag)
		} else {
			fmt.Printf("  LLM: %s (default)\n", cfg.DefaultLLM)
		}
		if embeddingFlag != "" {
			fmt.Printf("  Embedding: %s\n", embeddingFlag)
		} else {
			fmt.Printf("  Embedding: %s (default)\n", cfg.DefaultEmbedding)
		}
		return nil
	},
}

func init() {
	initCmd.Flags().String("llm", "", "LLM provider name (as defined in config)")
	initCmd.Flags().String("embedding", "", "Embedding provider name (as defined in config)")
	rootCmd.AddCommand(initCmd)
}
