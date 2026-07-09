package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Serve.Host != "127.0.0.1" {
		t.Errorf("Serve.Host = %q, want %q", cfg.Serve.Host, "127.0.0.1")
	}
	if cfg.Serve.Port != 8420 {
		t.Errorf("Serve.Port = %d, want %d", cfg.Serve.Port, 8420)
	}
	if cfg.DefaultWiki != "" {
		t.Errorf("DefaultWiki = %q, want empty", cfg.DefaultWiki)
	}

	// Provider registries should contain the default Ollama entries.
	if len(cfg.LLMProviders) != 1 {
		t.Errorf("LLMProviders has %d entries, want 1", len(cfg.LLMProviders))
	}
	ollamaLLM, ok := cfg.LLMProviders["ollama"]
	if !ok {
		t.Fatal("LLMProviders missing key 'ollama'")
	}
	if ollamaLLM.Provider != "ollama" {
		t.Errorf("LLMProviders[ollama].Provider = %q, want ollama", ollamaLLM.Provider)
	}
	if ollamaLLM.BaseURL != "http://localhost:11434" {
		t.Errorf("LLMProviders[ollama].BaseURL = %q, want http://localhost:11434", ollamaLLM.BaseURL)
	}
	if ollamaLLM.Model != "gpt-oss:20b" {
		t.Errorf("LLMProviders[ollama].Model = %q, want gpt-oss:20b", ollamaLLM.Model)
	}
	if ollamaLLM.Temperature != 0.3 {
		t.Errorf("LLMProviders[ollama].Temperature = %v, want 0.3", ollamaLLM.Temperature)
	}
	if ollamaLLM.MaxInputTokens != 128<<10 {
		t.Errorf("LLMProviders[ollama].MaxInputTokens = %d, want %d", ollamaLLM.MaxInputTokens, 128<<10)
	}

	if len(cfg.EmbeddingProviders) != 1 {
		t.Errorf("EmbeddingProviders has %d entries, want 1", len(cfg.EmbeddingProviders))
	}
	ollamaEmbed, ok := cfg.EmbeddingProviders["ollama-embed"]
	if !ok {
		t.Fatal("EmbeddingProviders missing key 'ollama-embed'")
	}
	if ollamaEmbed.Provider != "ollama" {
		t.Errorf("EmbeddingProviders[ollama-embed].Provider = %q, want ollama", ollamaEmbed.Provider)
	}
	if ollamaEmbed.BaseURL != "http://localhost:11434" {
		t.Errorf("EmbeddingProviders[ollama-embed].BaseURL = %v, want http://localhost:11434", ollamaEmbed.BaseURL)
	}
	if ollamaEmbed.Model != "nomic-embed-text" {
		t.Errorf("EmbeddingProviders[ollama-embed].Model = %q, want nomic-embed-text", ollamaEmbed.Model)
	}

	// Default provider names.
	if cfg.DefaultLLM != "ollama" {
		t.Errorf("DefaultLLM = %q, want ollama", cfg.DefaultLLM)
	}
	if cfg.DefaultEmbedding != "ollama-embed" {
		t.Errorf("DefaultEmbedding = %q, want ollama-embed", cfg.DefaultEmbedding)
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

func TestWikiEntryPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get home directory")
	}

	w := WikiEntry{Name: "my-notes"}
	expected := filepath.Join(home, ".ruminate", "my-notes")
	if w.Path() != expected {
		t.Errorf("Path() = %q, want %q", w.Path(), expected)
	}
}

func TestValidateWikiName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"my-notes", false},
		{"research", false},
		{"test_wiki", false},
		{"wiki123", false},
		{"", true},
		{"-invalid", true},
		{"_invalid", true},
		{"has space", true},
		{"中文", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWikiName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWikiName(%q) error = %v, wantErr = %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Should get defaults when no file exists.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil")
	}
	if cfg.Serve.Port != 8420 {
		t.Errorf("Serve.Port = %d, want %d", cfg.Serve.Port, 8420)
	}
	if cfg.DefaultLLM != "ollama" {
		t.Errorf("DefaultLLM = %q, want ollama", cfg.DefaultLLM)
	}

	// Save and reload with custom values.
	cfg.DefaultWiki = "my-notes"
	cfg.Wikis = append(cfg.Wikis, WikiEntry{Name: "my-notes", LLM: "ollama"})
	if err := Save(cfg); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg2.DefaultWiki != "my-notes" {
		t.Errorf("DefaultWiki = %q, want %q", cfg2.DefaultWiki, "my-notes")
	}
	if len(cfg2.Wikis) != 1 || cfg2.Wikis[0].Name != "my-notes" {
		t.Errorf("Wikis = %v, want [my-notes]", cfg2.Wikis)
	}
	if cfg2.Wikis[0].LLM != "ollama" {
		t.Errorf("Wiki LLM = %q, want ollama", cfg2.Wikis[0].LLM)
	}
	if cfg2.DefaultLLM != "ollama" {
		t.Errorf("DefaultLLM = %q, want ollama", cfg2.DefaultLLM)
	}
}

// =============================================================================
// Provider resolution tests
// =============================================================================

func TestResolveLLMConfig(t *testing.T) {
	global := &Config{
		LLMProviders: map[string]LLMConfig{
			"ollama":   {Provider: "ollama", Model: "gpt-oss:20b"},
			"deepseek": {Provider: "openai-compatible", Model: "deepseek-chat"},
		},
		DefaultLLM: "ollama",
	}

	t.Run("wiki overrides provider", func(t *testing.T) {
		entry := &WikiEntry{Name: "test", LLM: "deepseek"}
		cfg, err := ResolveLLMConfig(entry, global)
		if err != nil {
			t.Fatalf("ResolveLLMConfig() error: %v", err)
		}
		if cfg.Provider != "openai-compatible" {
			t.Errorf("Provider = %q, want openai-compatible", cfg.Provider)
		}
		if cfg.Model != "deepseek-chat" {
			t.Errorf("Model = %q, want deepseek-chat", cfg.Model)
		}
	})

	t.Run("wiki empty, uses global default", func(t *testing.T) {
		entry := &WikiEntry{Name: "test"}
		cfg, err := ResolveLLMConfig(entry, global)
		if err != nil {
			t.Fatalf("ResolveLLMConfig() error: %v", err)
		}
		if cfg.Provider != "ollama" {
			t.Errorf("Provider = %q, want ollama", cfg.Provider)
		}
	})

	t.Run("provider not found", func(t *testing.T) {
		entry := &WikiEntry{Name: "test", LLM: "nonexistent"}
		_, err := ResolveLLMConfig(entry, global)
		if err == nil {
			t.Fatal("ResolveLLMConfig() expected error for nonexistent provider")
		}
	})

	t.Run("no default set", func(t *testing.T) {
		entry := &WikiEntry{Name: "test"}
		noDefault := &Config{
			LLMProviders: map[string]LLMConfig{
				"ollama": {Provider: "ollama"},
			},
		}
		_, err := ResolveLLMConfig(entry, noDefault)
		if err == nil {
			t.Fatal("ResolveLLMConfig() expected error when no default set")
		}
	})
}

func TestResolveEmbeddingConfig(t *testing.T) {
	global := &Config{
		EmbeddingProviders: map[string]EmbeddingConfig{
			"ollama-embed": {Provider: "ollama", Model: "nomic-embed-text"},
			"openai-embed": {Provider: "openai", Model: "text-embedding-3-small"},
		},
		DefaultEmbedding: "ollama-embed",
	}

	t.Run("wiki overrides provider", func(t *testing.T) {
		entry := &WikiEntry{Name: "test", Embedding: "openai-embed"}
		cfg, err := ResolveEmbeddingConfig(entry, global)
		if err != nil {
			t.Fatalf("ResolveEmbeddingConfig() error: %v", err)
		}
		if cfg.Provider != "openai" {
			t.Errorf("Provider = %q, want openai", cfg.Provider)
		}
		if cfg.Model != "text-embedding-3-small" {
			t.Errorf("Model = %q, want text-embedding-3-small", cfg.Model)
		}
	})

	t.Run("wiki empty, uses global default", func(t *testing.T) {
		entry := &WikiEntry{Name: "test"}
		cfg, err := ResolveEmbeddingConfig(entry, global)
		if err != nil {
			t.Fatalf("ResolveEmbeddingConfig() error: %v", err)
		}
		if cfg.Provider != "ollama" {
			t.Errorf("Provider = %q, want ollama", cfg.Provider)
		}
	})

	t.Run("provider not found", func(t *testing.T) {
		entry := &WikiEntry{Name: "test", Embedding: "nonexistent"}
		_, err := ResolveEmbeddingConfig(entry, global)
		if err == nil {
			t.Fatal("ResolveEmbeddingConfig() expected error for nonexistent provider")
		}
	})
}

// =============================================================================
// Provider validation tests
// =============================================================================

func TestValidateLLMProvider(t *testing.T) {
	global := &Config{
		LLMProviders: map[string]LLMConfig{
			"ollama": {Provider: "ollama"},
		},
	}

	if err := ValidateLLMProvider(global, "ollama"); err != nil {
		t.Errorf("ValidateLLMProvider(ollama) = %v, want nil", err)
	}
	if err := ValidateLLMProvider(global, "nonexistent"); err == nil {
		t.Error("ValidateLLMProvider(nonexistent) expected error")
	}
}

func TestValidateEmbeddingProvider(t *testing.T) {
	global := &Config{
		EmbeddingProviders: map[string]EmbeddingConfig{
			"ollama-embed": {Provider: "ollama"},
		},
	}

	if err := ValidateEmbeddingProvider(global, "ollama-embed"); err != nil {
		t.Errorf("ValidateEmbeddingProvider(ollama-embed) = %v, want nil", err)
	}
	if err := ValidateEmbeddingProvider(global, "nonexistent"); err == nil {
		t.Error("ValidateEmbeddingProvider(nonexistent) expected error")
	}
}

// =============================================================================
// Runtime config resolution tests
// =============================================================================

func TestResolveRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// ResolveRuntimeConfig with empty name should error.
	_, err := ResolveRuntimeConfig("")
	if err == nil {
		t.Fatal("ResolveRuntimeConfig(\"\") expected error about empty wiki name")
	}

	// ResolveDefaultWikiName with no wikis registered — should error.
	_, err = ResolveDefaultWikiName()
	if err == nil {
		t.Fatal("ResolveDefaultWikiName() expected error with no wikis")
	}

	// Register one wiki with no provider overrides (inherits from global defaults).
	global := Default()
	global.Wikis = append(global.Wikis, WikiEntry{Name: "notes"})
	if err := Save(global); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	// Single wiki should auto-default.
	name, err := ResolveDefaultWikiName()
	if err != nil {
		t.Fatalf("ResolveDefaultWikiName() error: %v", err)
	}
	if name != "notes" {
		t.Errorf("ResolveDefaultWikiName() = %q, want %q", name, "notes")
	}

	rt, err := ResolveRuntimeConfig(name)
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig(%q) error: %v", name, err)
	}
	if rt.WikiName != "notes" {
		t.Errorf("WikiName = %q, want %q", rt.WikiName, "notes")
	}
	// Should resolve from global default provider.
	if rt.LLM.Provider != "ollama" {
		t.Errorf("LLM.Provider = %q, want ollama (from global default)", rt.LLM.Provider)
	}
	if rt.LLM.Model != "gpt-oss:20b" {
		t.Errorf("LLM.Model = %q, want gpt-oss:20b (from global default)", rt.LLM.Model)
	}

	// Unknown wiki.
	_, err = ResolveRuntimeConfig("nonexistent")
	if err == nil {
		t.Fatal("ResolveRuntimeConfig(\"nonexistent\") expected error")
	}

	// Multiple wikis with default set. Second wiki has a provider override.
	global.DefaultWiki = "notes"
	global.Wikis = append(global.Wikis, WikiEntry{Name: "research", LLM: "ollama"})
	if err := Save(global); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	// Without explicit, should resolve default.
	name, err = ResolveDefaultWikiName()
	if err != nil {
		t.Fatalf("ResolveDefaultWikiName() error: %v", err)
	}
	if name != "notes" {
		t.Errorf("ResolveDefaultWikiName() = %q, want %q (default)", name, "notes")
	}

	rt, err = ResolveRuntimeConfig(name)
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig(%q) error: %v", name, err)
	}
	if rt.WikiName != "notes" {
		t.Errorf("WikiName = %q, want %q (default)", rt.WikiName, "notes")
	}

	// Explicit still works — "research" has its own provider override defined in WikiEntry.
	rt, err = ResolveRuntimeConfig("research")
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig(\"research\") error: %v", err)
	}
	if rt.WikiName != "research" {
		t.Errorf("WikiName = %q, want %q", rt.WikiName, "research")
	}
	if rt.LLM.Provider != "ollama" {
		t.Errorf("LLM.Provider = %q, want ollama", rt.LLM.Provider)
	}

	// Multiple wikis without default should error.
	global.DefaultWiki = ""
	if err := Save(global); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}
	_, err = ResolveDefaultWikiName()
	if err == nil {
		t.Fatal("ResolveDefaultWikiName() expected error with multiple wikis and no default")
	}
}

func TestResolveRuntimeConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Set up global with a custom LLM provider and make it the default.
	global := Default()
	global.LLMProviders["deepseek"] = LLMConfig{
		Provider:       "openai-compatible",
		BaseURL:        "https://api.deepseek.com",
		Model:          "deepseek-chat",
		Temperature:    0.3,
		MaxInputTokens: 128 << 10,
	}
	global.DefaultLLM = "deepseek"
	global.Wikis = append(global.Wikis, WikiEntry{Name: "notes"})
	if err := Save(global); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	// Wiki has no explicit provider override — should inherit the global default "deepseek".
	rt, err := ResolveRuntimeConfig("notes")
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig() error: %v", err)
	}
	if rt.LLM.Provider != "openai-compatible" {
		t.Errorf("LLM.Provider = %q, want openai-compatible (global default)", rt.LLM.Provider)
	}
	if rt.LLM.Model != "deepseek-chat" {
		t.Errorf("LLM.Model = %q, want deepseek-chat (global default)", rt.LLM.Model)
	}
	if rt.LLM.BaseURL != "https://api.deepseek.com" {
		t.Errorf("LLM.BaseURL = %q, want https://api.deepseek.com", rt.LLM.BaseURL)
	}
	if rt.LLM.Temperature != 0.3 {
		t.Errorf("LLM.Temperature = %v, want 0.3", rt.LLM.Temperature)
	}
	if rt.LLM.MaxInputTokens != 128<<10 {
		t.Errorf("LLM.MaxInputTokens = %d, want %d", rt.LLM.MaxInputTokens, 128<<10)
	}
}
func TestWikiExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if WikiExists("nonexistent") {
		t.Error("WikiExists() should return false for nonexistent wiki")
	}

	// Create wiki directory
	wikiPath := filepath.Join(dir, ".ruminate", "test-wiki")
	if err := os.MkdirAll(wikiPath, 0755); err != nil {
		t.Fatalf("creating wiki dir: %v", err)
	}

	if !WikiExists("test-wiki") {
		t.Error("WikiExists() should return true for existing wiki")
	}
}
