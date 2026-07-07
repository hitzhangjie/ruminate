package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.WikiPath != "~/ruminate_wiki" {
		t.Errorf("WikiPath = %q, want %q", cfg.WikiPath, "~/ruminate_wiki")
	}
	if cfg.LLM.Provider != "ollama" {
		t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "ollama")
	}
	if cfg.LLM.BaseURL != "http://localhost:11434" {
		t.Errorf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, "http://localhost:11434")
	}
	if cfg.LLM.Model != "gpt-oss:20b" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "gpt-oss:20b")
	}
	if cfg.LLM.Temperature != 0.3 {
		t.Errorf("LLM.Temperature = %v, want %v", cfg.LLM.Temperature, 0.3)
	}
	if cfg.LLM.MaxInputTokens != 128<<10 {
		t.Errorf("LLM.MaxInputTokens = %d, want %d", cfg.LLM.MaxInputTokens, 128<<10)
	}
	if cfg.Embedding.Provider != "ollama" {
		t.Errorf("Embedding.Provider = %q, want %q", cfg.Embedding.Provider, "ollama")
	}
	if cfg.Embedding.BaseURL != "http://localhost:11434" {
		t.Errorf("Embedding.BaseURL = %q, want %q", cfg.Embedding.BaseURL, "http://localhost:11434")
	}
	if cfg.Embedding.Model != "nomic-embed-text" {
		t.Errorf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "nomic-embed-text")
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
	}
	if cfg.Server.Port != 8420 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8420)
	}
}

func TestExpandPath(t *testing.T) {
	t.Run("TestExpandPath", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("cannot get home directory")
		}

		tests := []struct {
			name       string
			input      string
			wantPrefix string
		}{
			{
				name:       "tilde expansion",
				input:      "~/documents",
				wantPrefix: filepath.Join(home, "documents"),
			},
			{
				name:       "no tilde, no env",
				input:      "/tmp/foo",
				wantPrefix: "/tmp/foo",
			},
			{
				name:       "empty path",
				input:      "",
				wantPrefix: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := ExpandPath(tt.input)
				if got != tt.wantPrefix {
					t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.wantPrefix)
				}
			})
		}
	})

	t.Run("TestExpandPath_EnvVar", func(t *testing.T) {
		t.Setenv("RUMINATE_TEST_DIR", "/fake/dir")
		got := ExpandPath("$RUMINATE_TEST_DIR/sub")
		if got != "/fake/dir/sub" {
			t.Errorf("ExpandPath = %q, want %q", got, "/fake/dir/sub")
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("TestLoad_DefaultConfigWhenNoFile", func(t *testing.T) {
		dir := t.TempDir()
		oldDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(oldDir)

		// Point viper to a nonexistent directory to force defaults.
		// Both cwd and $HOME are redirected so all three search paths
		// (., $HOME/.config/ruminate, $HOME) land in an empty temp dir.
		t.Setenv("HOME", dir)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg == nil {
			t.Fatal("Load() returned nil config")
		}
		// With no config file, defaults should be used (WikiPath is expanded)
		if cfg.WikiPath == "" {
			t.Error("WikiPath should not be empty")
		}
		if cfg.LLM.Provider != "ollama" {
			t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "ollama")
		}
	})

	t.Run("TestLoad_WithConfigFile", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("HOME", dir) // isolate from real home directory
		configContent := `
wiki_path: "/test/wiki"
llm:
  provider: "openai"
  base_url: "https://api.openai.com"
  model: "gpt-4"
  temperature: 0.7
embedding:
  provider: "openai"
  base_url: "https://api.openai.com"
  model: "text-embedding-3-small"
server:
  host: "0.0.0.0"
  port: 9000
`
		if err := os.WriteFile(filepath.Join(dir, ".ruminate.yaml"), []byte(configContent), 0644); err != nil {
			t.Fatalf("creating config file: %v", err)
		}

		oldDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(oldDir)

		// Point viper to a nonexistent directory to force defaults.
		// Both cwd and $HOME are redirected so all three search paths
		// (., $HOME/.config/ruminate, $HOME) land in an empty temp dir.
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		if cfg.WikiPath != "/test/wiki" {
			t.Errorf("WikiPath = %q, want %q", cfg.WikiPath, "/test/wiki")
		}
		if cfg.LLM.Provider != "openai" {
			t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "openai")
		}
		if cfg.LLM.Model != "gpt-4" {
			t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "gpt-4")
		}
		if cfg.Server.Port != 9000 {
			t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 9000)
		}
	})
}
