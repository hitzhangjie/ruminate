// Package query provides query capabilities for the Ruminate wiki:
// full-text search (find) and AI-powered question answering (ask).
package query

import (
	"fmt"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// Engine drives AI-powered query operations (ask).
//
// Engine is a higher-level orchestration component built on top of wiki.Manager.
// It owns the Manager lifecycle and coordinates the ask pipeline:
// retrieve context → build prompt → call LLM → optionally save result.
//
// Engine holds its own LLM provider reference rather than going through
// wiki.Manager — Manager's LLM provider serves write-path needs (e.g. embeddings),
// while Engine's provider serves read-path orchestration (chat inference).
//
// For simple full-text search without AI (find command), use wiki.Manager
// directly — no Engine is needed.
type Engine struct {
	wiki        *wiki.Manager
	llmProvider llm.LLMProvider
	llmCfg      config.LLMConfig
}

// NewEngine creates a new query Engine from the given configuration.
// It internally initializes the wiki.Manager and validates that the wiki
// is initialized. Returns an error if the wiki has not been initialized yet.
// The LLM provider is initialized from cfg; if unavailable, it stays nil
// (callers should check before calling Ask/AskStream).
func NewEngine(cfg *config.Config) (*Engine, error) {
	mgr := wiki.NewManager(cfg)
	if !mgr.IsInitialized() {
		return nil, fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
	}

	// Initialize LLM provider for this engine's orchestration needs.
	// Non-fatal: provider stays nil if unreachable, and Ask/AskStream
	// will return a clear error rather than panicking.
	var llmProvider llm.LLMProvider
	if cfg.LLM.Provider != "" {
		provider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model)
		if err == nil {
			llmProvider = provider
		}
	}

	return &Engine{
		wiki:        mgr,
		llmProvider: llmProvider,
		llmCfg:      cfg.LLM,
	}, nil
}
