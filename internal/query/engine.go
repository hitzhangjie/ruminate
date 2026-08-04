// Package query provides query capabilities for the Ruminate wiki:
// full-text search (find) and AI-powered question answering (ask).
package query

import (
	"context"
	"fmt"

	"github.com/hitzhangjie/ruminate/internal/agent"
	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/trace"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// wikiManager defines the subset of wiki.Manager methods used by Engine.
// wiki.Manager implicitly satisfies this interface.
type wikiManager interface {
	Search(ctx context.Context, query string, topN int, effort wiki.SearchEffort) ([]wiki.SearchResult, error)
	SearchRaw(query string, topN int) ([]wiki.SearchResult, error)
	ReadByPath(path string) (*wiki.Page, error)
	Read(title string, pageType wiki.PageType) (*wiki.Page, error)
	Create(title string, pageType wiki.PageType, content string) (*wiki.Page, error)
	Update(title string, pageType wiki.PageType, content string) (*wiki.Page, error)
	Index() *wiki.IndexManager
	SetTracer(tr *trace.Tracer)
}

// Engine drives AI-powered query operations (ask and agent).
//
// Engine is a higher-level orchestration component built on top of wiki.Manager.
// It owns the Manager lifecycle and coordinates both query paths:
//   - Ask pipeline: retrieve context → build prompt → call LLM → optionally save result.
//   - Agent (ReAct): multi-step exploration with tools (wiki/raw/code).
type Engine struct {
	wiki        wikiManager
	llmProvider llm.LLMProvider
	llmCfg      config.LLMConfig
	tracer      *trace.Tracer
	explorer    *agent.Explorer
}

// NewEngine creates a new query Engine from the given runtime configuration.
func NewEngine(cfg *config.RuntimeConfig) (*Engine, error) {
	mgr, err := wiki.NewManagerFromConfig(cfg.WikiPath, cfg.LLM, cfg.Embedding)
	if err != nil {
		return nil, err
	}
	if !mgr.IsInitialized() {
		return nil, fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", cfg.WikiPath)
	}

	var llmProvider llm.LLMProvider
	if cfg.LLM.Provider != "" {
		provider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKey)
		if err == nil {
			llmProvider = provider
		}
	}

	return &Engine{
		wiki:        mgr,
		llmProvider: llmProvider,
		llmCfg:      cfg.LLM,
		explorer:    agent.NewExplorer(mgr, llmProvider, cfg.LLM),
	}, nil
}

// SetTracer attaches a tracer to both the Engine and its underlying Manager.
// Pass nil to disable tracing.
func (e *Engine) SetTracer(tr *trace.Tracer) {
	e.tracer = tr
	e.wiki.SetTracer(tr)
	e.explorer.SetTracer(tr)
}

func (e *Engine) Tracer() *trace.Tracer {
	return e.tracer
}

// SaveAnswer saves a Q&A result as a wiki synthesis page.
func (e *Engine) SaveAnswer(question, answer string, refs []wiki.Ref) error {
	title, content := wiki.FormatSynthesisContent(question, answer, refs)

	existing, err := e.wiki.Read(title, wiki.PageTypeSynthesis)
	if err != nil {
		// Not found — create a new page.
		_, err = e.wiki.Create(title, wiki.PageTypeSynthesis, content)
		return err
	}
	// Already exists — update with new answer.
	_, err = e.wiki.Update(existing.Title, wiki.PageTypeSynthesis, content)
	return err
}
