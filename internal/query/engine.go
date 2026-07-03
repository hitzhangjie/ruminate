// Package query provides query capabilities for the Ruminate wiki:
// full-text search (find) and AI-powered question answering (ask).
package query

import (
	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// Engine drives query operations: find (full-text search) and ask (AI Q&A).
//
// Both find and ask use the SQLite FTS5 index (pages_fts) for retrieval.
// index.md is NOT used for search — it's a human-readable directory derived
// from pages_fts. See wiki.IndexManager for details.
type Engine struct {
	wiki   *wiki.Manager
	llm    llm.LLMProvider
	llmCfg config.LLMConfig
}

// NewEngine creates a new query Engine.
func NewEngine(w *wiki.Manager, llmProvider llm.LLMProvider, llmCfg config.LLMConfig) *Engine {
	return &Engine{
		wiki:   w,
		llm:    llmProvider,
		llmCfg: llmCfg,
	}
}
