package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds all configuration for Ruminate.
type Config struct {
	// Wiki is the path to the wiki root directory.
	WikiPath string `yaml:"wiki_path" mapstructure:"wiki_path"`

	// LLM provider configuration.
	LLM LLMConfig `yaml:"llm" mapstructure:"llm"`

	// Embedding provider configuration.
	Embedding EmbeddingConfig `yaml:"embedding" mapstructure:"embedding"`

	// Server configuration.
	Server ServerConfig `yaml:"server" mapstructure:"server"`
}

// LLMConfig holds configuration for the LLM inference provider.
type LLMConfig struct {
	// Provider is the LLM backend: "ollama", "openai", "deepseek".
	Provider string `yaml:"provider" mapstructure:"provider"`

	// BaseURL is the API base URL for the LLM provider.
	BaseURL string `yaml:"base_url" mapstructure:"base_url"`

	// Model is the model name to use for inference.
	Model string `yaml:"model" mapstructure:"model"`

	// Temperature controls randomness (0-1).
	Temperature float64 `yaml:"temperature" mapstructure:"temperature"`

	// MaxInputTokens is the maximum number of tokens the model can accept as input.
	// Content exceeding this limit will be rejected with an error rather than
	// silently truncated. Different models have different context windows:
	// llama3.2 (~128K), DeepSeek (~1M), GPT-4o (~128K), etc.
	// Set this based on your model's context window, leaving room for the system
	// prompt and response. 0 means no limit (not recommended).
	//
	// TODO: 这里最好能够根据模型信息自动进行调整，而不是完全依赖这里的配置，这里的配置应该是一个默认的最大输入token
	MaxInputTokens int `yaml:"max_input_tokens" mapstructure:"max_input_tokens"`
}

// EmbeddingConfig holds configuration for the embedding provider.
type EmbeddingConfig struct {
	// Provider is the embedding backend.
	Provider string `yaml:"provider" mapstructure:"provider"`

	// BaseURL is the API base URL for the embedding provider.
	BaseURL string `yaml:"base_url" mapstructure:"base_url"`

	// Model is the model name to use for embeddings.
	Model string `yaml:"model" mapstructure:"model"`
}

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	// Host is the listen address.
	Host string `yaml:"host" mapstructure:"host"`

	// Port is the listen port.
	Port int `yaml:"port" mapstructure:"port"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		WikiPath: "ruminate_wiki",
		LLM: LLMConfig{
			Provider:       "ollama",
			BaseURL:        "http://localhost:11434",
			Model:          "gpt-oss:20b",
			Temperature:    0.3,
			MaxInputTokens: 128 << 10,
		},
		Embedding: EmbeddingConfig{
			Provider: "ollama",
			BaseURL:  "http://localhost:11434",
			Model:    "nomic-embed-text",
		},
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8420,
		},
	}
}

// Load loads configuration from file, environment, and defaults.
//
// Search order (last wins):
//  1. Default values
//  2. Config file: .ruminate.yaml (current dir), ~/.config/ruminate/config.yaml
//  3. Environment variables: RUMINATE_* (e.g. RUMINATE_WIKI_PATH)
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	cfg := DefaultConfig()
	v.SetDefault("wiki_path", cfg.WikiPath)
	v.SetDefault("llm", cfg.LLM)
	v.SetDefault("embedding", cfg.Embedding)
	v.SetDefault("server", cfg.Server)

	// Config file search
	v.SetConfigName(".ruminate")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")                      // current directory
	v.AddConfigPath("$HOME/.config/ruminate") // user config dir
	v.AddConfigPath("$HOME")                  // home directory fallback

	// Environment variables
	v.SetEnvPrefix("RUMINATE")
	v.AutomaticEnv()

	// Try to read config file (not an error if missing)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		// File not found: use defaults silently
	}

	// Unmarshal into config struct
	loaded := &Config{}
	if err := v.Unmarshal(loaded); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Expand path variables
	loaded.WikiPath = expandPath(loaded.WikiPath)

	return loaded, nil
}

// expandPath expands ~ and environment variables in a path.
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	return os.ExpandEnv(path)
}
