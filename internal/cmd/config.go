package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Ruminate configuration",
	Long:  `View and edit Ruminate configuration. All settings are stored in one file ($HOME/.ruminate/config.yaml).`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the full configuration",
	Long:  `Display the full configuration file content.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		print(cfg)
		return nil
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit configuration in your default editor",
	Long: `Open the configuration file ($HOME/.ruminate/config.yaml) in $EDITOR
(falls back to vim, then nano).

All settings are in this single file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := filepath.Join(config.ExpandPath("~/.ruminate"), "config.yaml")
		// Create default config if it doesn't exist.
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if err := config.Save(config.Default()); err != nil {
				return fmt.Errorf("creating default config: %w", err)
			}
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			// Fall back to common editors.
			for _, e := range []string{"vim", "nano", "vi"} {
				if _, err := exec.LookPath(e); err == nil {
					editor = e
					break
				}
			}
		}
		if editor == "" {
			return fmt.Errorf("no editor found — set $EDITOR or install vim/nano.\nConfig file is at: %s", configPath)
		}

		c := exec.Command(editor, configPath)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("editor exited with error: %w", err)
		}

		fmt.Println("Config saved. Changes take effect on next command.")
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set individual config fields",
	Long:  `Set individual default fields in the configuration, such as the default wiki, LLM provider, or embedding provider.`,
}

var configSetDefaultWikiCmd = &cobra.Command{
	Use:   "default-wiki <name>",
	Short: "Set the default wiki",
	Long:  `Set which wiki is used when --wiki is not specified.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.ValidateWikiName(name); err != nil {
			return err
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		found := false
		for _, w := range cfg.Wikis {
			if w.Name == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("wiki %q is not registered. Known wikis: run 'ruminate config show'", name)
		}

		cfg.DefaultWiki = name
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Printf("Default wiki set to %q.\n", name)
		return nil
	},
}

var configSetDefaultLLMCmd = &cobra.Command{
	Use:   "default-llm <provider>",
	Short: "Set the default LLM provider",
	Long:  `Set the default LLM provider used when a wiki has no explicit LLM override.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if err := config.ValidateLLMProvider(cfg, name); err != nil {
			return err
		}

		cfg.DefaultLLM = name
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Printf("Default LLM provider set to %q.\n", name)
		return nil
	},
}

var configSetDefaultEmbeddingCmd = &cobra.Command{
	Use:   "default-embedding <provider>",
	Short: "Set the default embedding provider",
	Long:  `Set the default embedding provider used when a wiki has no explicit embedding override.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if err := config.ValidateEmbeddingProvider(cfg, name); err != nil {
			return err
		}

		cfg.DefaultEmbedding = name
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Printf("Default embedding provider set to %q.\n", name)
		return nil
	},
}

func init() {
	configSetCmd.AddCommand(configSetDefaultWikiCmd)
	configSetCmd.AddCommand(configSetDefaultLLMCmd)
	configSetCmd.AddCommand(configSetDefaultEmbeddingCmd)

	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configSetCmd)
}
