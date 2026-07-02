// Package ingest implements the knowledge ingestion pipeline for Ruminate.
//
// # Overview
//
// The ingest pipeline reads source material (local files or URLs), sends it
// to an LLM for structured analysis, and writes the extracted knowledge into
// the wiki as interlinked Markdown pages. Each ingest run is versioned via
// Git commit.
//
// # Pipeline
//
// A source flows through these stages:
//
//	Source (raw text)
//	  │
//	  ├─ [1] Read ────────────── Reader reads file/URL into Source struct
//	  │
//	  ├─ [2] Save raw copy ──── Archives to raw/{type}/{title}.md and
//	  │                         indexes in FTS5 for full-text search.
//	  │
//	  ├─ [3] LLM analyze ────── LLM decomposes content into AnalysisResult
//	  │
//	  ├─ [4] Summary page ───── Summary + KeyPoints + Tags → summaries:{title}
//	  │                         Links back to the raw source (and original URL).
//	  │
//	  ├─ [5] Entity pages ───── Entities → entities:{name} (deduped by name)
//	  │
//	  ├─ [6] Concept pages ──── Concepts → concepts:{name} (deduped by name)
//	  │
//	  └─ [7] Git commit ─────── All changes committed atomically
//
// # Traceability
//
// Every piece of structured knowledge links back to its source:
//
//	entity/concept page
//	  │  [[summaries:{title}]] — WikiLink to the summary page
//	  ▼
//	summary page
//	  │  [📄 raw](../../raw/{type}/{title}.md) — direct link to raw archive
//	  │  (plus the original URL when applicable)
//	  ▼
//	raw/{type}/{title}.md  — the original source, also searchable via FTS5
//
// Users can trace any claim back to the original text in at most two clicks,
// and raw sources are full-text searchable alongside wiki pages.
//
// # Key Types
//
// Source is the raw input: title, full text content, user-labeled type
// ("article", "paper", "note", "book"), and origin path/URL.
//
// AnalysisResult is the structured knowledge extracted by the LLM. It is
// the bridge between "raw text" and "wiki pages" — the LLM decomposes a
// source into five components, each mapping to a different part of the wiki:
//
//	Field       Wiki destination
//	Summary     → Summary page body
//	KeyPoints   → Summary page bullet list
//	Entities    → Standalone entity pages ([[entities:...]])
//	Concepts    → Standalone concept pages ([[concepts:...]])
//	Tags        → Summary page inline tags (for filtering/search)
//
// # Entities vs Concepts
//
// Entities are concrete, identifiable things: people, events, technical
// terms, organizations. Concepts are abstract ideas, themes, or frameworks
// (e.g. "Technical Debt", "Attention Mechanism").
//
// Both are long-lived wiki objects. When a subsequent ingest extracts the
// same entity or concept name, the new description is appended as a new
// section rather than replacing the existing page, so knowledge accumulates
// across ingest runs.
package ingest
