// Package query provides query capabilities for the Ruminate wiki:
// full-text search (find) and AI-powered question answering (ask).
package query

import (
	"context"
	"fmt"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/trace"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// wikiManager defines the subset of wiki.Manager methods used by Engine.
// wiki.Manager implicitly satisfies this interface.
type wikiManager interface {
	Search(ctx context.Context, query string, topN int, effort wiki.SearchEffort) ([]wiki.SearchResult, error)
	ReadByPath(path string) (*wiki.Page, error)
	Read(title string, pageType wiki.PageType) (*wiki.Page, error)
	Create(title string, pageType wiki.PageType, content string) (*wiki.Page, error)
	Update(title string, pageType wiki.PageType, content string) (*wiki.Page, error)
	Index() *wiki.IndexManager
	SetTracer(tr *trace.Tracer)
}

// Engine drives AI-powered query operations (ask).
//
// Engine is a higher-level orchestration component built on top of wiki.Manager.
// It owns the Manager lifecycle and coordinates the ask pipeline:
// retrieve context → build prompt → call LLM → optionally save result.
type Engine struct {
	wiki        wikiManager
	llmProvider llm.LLMProvider
	llmCfg      config.LLMConfig
	tracer      *trace.Tracer
}

// NewEngine creates a new query Engine from the given configuration.
func NewEngine(cfg *config.Config) (*Engine, error) {
	mgr := wiki.NewManager(cfg)
	if !mgr.IsInitialized() {
		return nil, fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
	}

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

// SetTracer attaches a tracer to both the Engine and its underlying Manager.
// Pass nil to disable tracing.
func (e *Engine) SetTracer(tr *trace.Tracer) {
	e.tracer = tr
	e.wiki.SetTracer(tr)
}
