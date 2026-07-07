# CLAUDE.md

This file provides guidance to Claude Code when working in this repository.

## Project Overview

**Ruminate**（反刍）is an AI-driven personal knowledge base system. It incrementally builds and maintains a persistent, interlinked Markdown wiki — curated by the user, written and maintained entirely by LLMs.

Core insight (from [Karpathy's LLM Wiki](llm_wiki.md)): The hard part of maintaining a knowledge base isn't reading or thinking — it's the bookkeeping. LLMs never get bored, never forget to update cross-references, and can touch 15 files in one pass.

## Key Documents

- [README.md](README.md) — project vision and overview (Chinese)
- [llm_wiki.md](llm_wiki.md) — Karpathy's original LLM Wiki idea (English)
- [docs/1-requirements.md](docs/1-requirements.md) — user needs analysis and consensus
- [docs/2-architecture.md](docs/2-architecture.md) — technical architecture and design decisions
- [docs/3-tasks.md](docs/3-tasks.md) — task breakdown and progress tracking

## Tech Stack

| Layer | Technology | Reason |
|-------|-----------|--------|
| Backend | Go | Developer familiarity; strong CLI and concurrency support |
| Frontend | TypeScript + React (Vite) | AI-friendly; developer can read and contribute |
| Storage | Markdown + Git + SQLite FTS5 | Git-native version control; embedded full-text search |
| LLM Provider (P0) | Ollama (inference + embedding) | Local-first; unified provider for both needs |
| LLM Provider (P1+) | DeepSeek, OpenAI-compatible | Expand to cloud providers |
| CLI Framework | Cobra | Standard Go CLI library |

## Directory Structure (Planned)

```
ruminate/
├── cmd/                    # CLI entry points (cobra commands)
├── internal/
│   ├── ingest/             # Ingestion engine
│   ├── query/              # Query engine (find + ask)
│   ├── lint/               # Wiki health check engine
│   ├── wiki/               # Wiki CRUD, index, log management
│   ├── llm/                # LLM provider abstraction (inference + embedding)
│   ├── search/             # SQLite FTS5 search
│   ├── git/                # Git operations wrapper
│   ├── config/             # Configuration management
│   └── serve/              # HTTP server + API
├── web/                    # Frontend (Vite + React + TypeScript)
├── docs/                   # Project documentation
├── testdata/               # Test fixtures
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Architecture Principles

1. **Local-first**: All data on user's disk. No cloud dependency.
2. **Git-native**: Wiki is a git repo. Every change is versioned and rollback-able.
3. **Markdown as source of truth**: Wiki pages are plain Markdown. Human-readable with any editor (Obsidian, VS Code, etc.).
4. **Provider abstraction**: LLM and embedding interfaces are abstracted so providers can be swapped (Ollama → DeepSeek → OpenAI) without changing core logic.
5. **CLI first, then Web**: Core operations via CLI. Web UI layers on top via HTTP API.
6. **Incremental adoption**: Start simple. Add features as proven necessary.

## Development Commands

```bash
make build       # Build the CLI binary
make test        # Run all tests
make lint        # Run linters
make dev         # Start dev server (backend + frontend)
make install     # Install CLI to $GOPATH/bin
```

## Language and Style

- Commit messages in English (conventional commits format)
- Code comments in English
- Documentation in Chinese where user-facing, English where developer-facing
- Follow Go standard project layout conventions
