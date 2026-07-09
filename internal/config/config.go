package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// validWikiName is the regex for valid wiki names.
var validWikiName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// =============================================================================
// Sub-config types
// =============================================================================

// LLMConfig holds configuration for the LLM inference provider.
type LLMConfig struct {
	Provider       string  `yaml:"provider"`
	BaseURL        string  `yaml:"base_url"`
	Model          string  `yaml:"model"`
	Temperature    float64 `yaml:"temperature"`
	MaxInputTokens int     `yaml:"max_input_tokens"`
}

// EmbeddingConfig holds configuration for the embedding provider.
type EmbeddingConfig struct {
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	Model    string `yaml:"model"`
}

// ServeConfig holds configuration for the HTTP server.
type ServeConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// =============================================================================
// Configuration ($HOME/.ruminate/config.yaml)
// =============================================================================

// Config is the top-level configuration stored at $HOME/.ruminate/config.yaml.
// It holds the wiki registry (each entry can override LLM/embedding providers),
// default wiki, HTTP serve configuration, named LLM/embedding provider registries,
// and default provider selections.
//
// Each WikiEntry may specify an optional LLM and/or Embedding provider name.
// When empty, the wiki inherits DefaultLLM / DefaultEmbedding.
type Config struct {
	Wikis              []WikiEntry                `yaml:"wikis"`
	LLMProviders       map[string]LLMConfig       `yaml:"llm_providers,omitempty"`
	EmbeddingProviders map[string]EmbeddingConfig `yaml:"embedding_providers,omitempty"`

	DefaultWiki      string `yaml:"default_wiki"`
	DefaultLLM       string `yaml:"default_llm,omitempty"`
	DefaultEmbedding string `yaml:"default_embedding,omitempty"`

	Serve ServeConfig `yaml:"serve"`
}

// WikiEntry represents a registered wiki.
// LLM and Embedding fields are optional provider name references;
// empty means "inherit the default".
type WikiEntry struct {
	Name      string `yaml:"name"`
	LLM       string `yaml:"llm,omitempty"`
	Embedding string `yaml:"embedding,omitempty"`
}

// Path returns the filesystem path for this wiki ($HOME/.ruminate/<name>).
func (w WikiEntry) Path() string {
	return filepath.Join(ruminateDir(), w.Name)
}

// =============================================================================
// Runtime configuration (combined for a single command execution)
// =============================================================================

// RuntimeConfig is the combined configuration for a single command execution.
// It resolves provider references from the wiki entry against the provider
// registries so downstream engines receive fully resolved configs.
type RuntimeConfig struct {
	// Current wiki identity
	WikiName string
	WikiPath string // derived: $HOME/.ruminate/<WikiName>

	// Resolved provider configurations (wiki entry name → provider registry lookup)
	LLM       LLMConfig
	Embedding EmbeddingConfig

	// Shared configuration
	Serve ServeConfig

	// Full wiki registry (needed by serve, config list, sync --all)
	Wikis       []WikiEntry
	DefaultWiki string
}

// =============================================================================
// Default factories
// =============================================================================

// Default returns the default configuration with a single Ollama provider
// registered for both LLM and embedding. Users can add more providers via
// "ruminate config edit" or by editing ~/.ruminate/config.yaml.
func Default() *Config {
	return &Config{
		Serve: ServeConfig{
			Host: "127.0.0.1",
			Port: 8420,
		},
		LLMProviders: map[string]LLMConfig{
			"ollama": {
				Provider:       "ollama",
				BaseURL:        "http://localhost:11434",
				Model:          "gpt-oss:20b",
				Temperature:    0.3,
				MaxInputTokens: 128 << 10,
			},
		},
		EmbeddingProviders: map[string]EmbeddingConfig{
			"ollama-embed": {
				Provider: "ollama",
				BaseURL:  "http://localhost:11434",
				Model:    "nomic-embed-text",
			},
		},
		DefaultLLM:       "ollama",
		DefaultEmbedding: "ollama-embed",
	}
}

// =============================================================================
// Path helpers
// =============================================================================

// ruminateDir returns the Ruminate data directory ($HOME/.ruminate).
func ruminateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback: use current directory
		return filepath.Join(".", ".ruminate")
	}
	return filepath.Join(home, ".ruminate")
}

// ExpandPath expands ~ and environment variables in a path.
func ExpandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	return os.ExpandEnv(path)
}

// =============================================================================
// Config loading
// =============================================================================

// Load reads the config from $HOME/.ruminate/config.yaml.
// It starts from DefaultConfig and overlays file content on top.
func Load() (*Config, error) {
	cfg := Default()

	path := filepath.Join(ruminateDir(), "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Ensure maps are initialized even if YAML had no providers section.
	if cfg.LLMProviders == nil {
		cfg.LLMProviders = make(map[string]LLMConfig)
	}
	if cfg.EmbeddingProviders == nil {
		cfg.EmbeddingProviders = make(map[string]EmbeddingConfig)
	}

	return cfg, nil
}

// Save writes the config to $HOME/.ruminate/config.yaml.
// Creates the directory if it doesn't exist.
func Save(cfg *Config) error {
	dir := ruminateDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}

	return nil
}

// =============================================================================
// Provider resolution
// =============================================================================

// ResolveLLMConfig resolves the LLM provider for a wiki entry from the provider registry.
// Priority: wiki entry override name > DefaultLLM.
func ResolveLLMConfig(entry *WikiEntry, cfg *Config) (LLMConfig, error) {
	name := entry.LLM
	if name == "" {
		name = cfg.DefaultLLM
	}
	if name == "" {
		return LLMConfig{}, fmt.Errorf("no LLM provider configured: set default_llm in config or specify --llm during init")
	}
	provider, ok := cfg.LLMProviders[name]
	if !ok {
		available := make([]string, 0, len(cfg.LLMProviders))
		for n := range cfg.LLMProviders {
			available = append(available, n)
		}
		return LLMConfig{}, fmt.Errorf("LLM provider %q not found (available: %v)", name, available)
	}
	return provider, nil
}

// ResolveEmbeddingConfig resolves the embedding provider for a wiki entry from the provider registry.
// Priority: wiki entry override name > DefaultEmbedding.
func ResolveEmbeddingConfig(entry *WikiEntry, cfg *Config) (EmbeddingConfig, error) {
	name := entry.Embedding
	if name == "" {
		name = cfg.DefaultEmbedding
	}
	if name == "" {
		return EmbeddingConfig{}, fmt.Errorf("no embedding provider configured: set default_embedding in config or specify --embedding during init")
	}
	provider, ok := cfg.EmbeddingProviders[name]
	if !ok {
		available := make([]string, 0, len(cfg.EmbeddingProviders))
		for n := range cfg.EmbeddingProviders {
			available = append(available, n)
		}
		return EmbeddingConfig{}, fmt.Errorf("embedding provider %q not found (available: %v)", name, available)
	}
	return provider, nil
}

// ValidateLLMProvider checks whether the named provider exists in the provider registry.
func ValidateLLMProvider(cfg *Config, name string) error {
	if _, ok := cfg.LLMProviders[name]; !ok {
		available := make([]string, 0, len(cfg.LLMProviders))
		for n := range cfg.LLMProviders {
			available = append(available, n)
		}
		return fmt.Errorf("LLM provider %q not found in config (available: %v)", name, available)
	}
	return nil
}

// ValidateEmbeddingProvider checks whether the named provider exists in the provider registry.
func ValidateEmbeddingProvider(cfg *Config, name string) error {
	if _, ok := cfg.EmbeddingProviders[name]; !ok {
		available := make([]string, 0, len(cfg.EmbeddingProviders))
		for n := range cfg.EmbeddingProviders {
			available = append(available, n)
		}
		return fmt.Errorf("embedding provider %q not found in config (available: %v)", name, available)
	}
	return nil
}

// =============================================================================
// Wiki resolution
// =============================================================================

// ResolveDefaultWikiName determines which wiki to use when none is explicitly
// specified. It loads the config and applies the default resolution logic:
//
//  1. Config.DefaultWiki
//  2. If only one wiki is registered, auto-default
//  3. Error: ambiguous (multiple wikis, no default)
//
// Callers should use this when wikiName is empty, then pass the result to
// ResolveRuntimeConfig.
func ResolveDefaultWikiName() (string, error) {
	cfg, err := Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}

	// 1. default_wiki
	if cfg.DefaultWiki != "" {
		for _, w := range cfg.Wikis {
			if w.Name == cfg.DefaultWiki {
				return w.Name, nil
			}
		}
	}

	// 2. Only one wiki — auto-default
	if len(cfg.Wikis) == 1 {
		return cfg.Wikis[0].Name, nil
	}

	// 3. Ambiguous
	return "", fmt.Errorf(
		"multiple wikis found and no default set. Use --wiki or set a default:\n  ruminate config set default-wiki <name>",
	)
}

// ResolveRuntimeConfig resolves a specific wiki by name and returns a combined RuntimeConfig.
// wikiName must not be empty — callers should resolve the default wiki via
// ResolveDefaultWikiName before calling this function.
func ResolveRuntimeConfig(wikiName string) (*RuntimeConfig, error) {
	if wikiName == "" {
		return nil, fmt.Errorf("wiki name must not be empty — resolve the default wiki via ResolveDefaultWikiName before calling ResolveRuntimeConfig")
	}

	cfg, err := Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	// Find the wiki entry in the registry.
	var target *WikiEntry
	for i := range cfg.Wikis {
		if cfg.Wikis[i].Name == wikiName {
			target = &cfg.Wikis[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("wiki %q not found. Known wikis: %v", wikiName, wikiNames(cfg.Wikis))
	}

	// Resolve provider references from the wiki entry against the provider registries.
	llmCfg, err := ResolveLLMConfig(target, cfg)
	if err != nil {
		return nil, fmt.Errorf("resolving LLM config for wiki %q: %w", target.Name, err)
	}

	embedCfg, err := ResolveEmbeddingConfig(target, cfg)
	if err != nil {
		return nil, fmt.Errorf("resolving embedding config for wiki %q: %w", target.Name, err)
	}

	rt := &RuntimeConfig{
		WikiName:    target.Name,
		WikiPath:    target.Path(),
		LLM:         llmCfg,
		Embedding:   embedCfg,
		Serve:       cfg.Serve,
		Wikis:       cfg.Wikis,
		DefaultWiki: cfg.DefaultWiki,
	}

	return rt, nil
}

// wikiNames returns a slice of wiki names from the entries.
func wikiNames(entries []WikiEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

// =============================================================================
// Validation
// =============================================================================

// ValidateWikiName checks if the given name is a valid wiki name.
// Valid names: alphanumeric, underscores, hyphens. Must start with alphanumeric.
func ValidateWikiName(name string) error {
	if name == "" {
		return fmt.Errorf("wiki name must not be empty")
	}
	if !validWikiName.MatchString(name) {
		return fmt.Errorf("invalid wiki name %q: must start with a letter or digit and contain only letters, digits, underscores, and hyphens", name)
	}
	return nil
}

// WikiExists checks whether a wiki with the given name already exists on disk.
func WikiExists(name string) bool {
	path := filepath.Join(ruminateDir(), name)
	_, err := os.Stat(path)
	return err == nil
}
