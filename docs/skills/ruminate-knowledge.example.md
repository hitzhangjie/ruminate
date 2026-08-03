# Ruminate Knowledge Plane — Agent Skill 示例

> 将本文件内容（或改编后）放入宿主 agent 的规则/skills 中：  
> `AGENTS.md` / `CLAUDE.md` / Cursor rules / 自定义 skill。  
> 设计依据：[109-agent-exploration.md](../109-agent-exploration.md)

---

## When to use

Use this skill when answering questions that may be covered by a Ruminate wiki, or when you need compiled knowledge before diving into a large codebase.

## Locations (fill in per project)

```text
RUMINATE_WIKI_ROOT = /path/to/my-wiki
  wiki/     — compiled knowledge (Synthesis)
  raw/      — immutable evidence archives (Evidence)
  index.md  — human-readable catalog

CODE_ROOTS =
  - /path/to/service-a
  - /path/to/service-b
```

## Preferred strategy

1. **L1 — Wiki first**  
   - Run: `ruminate find "<keywords>"` and/or `ruminate ask "<question>" --evidence auto`  
   - Prefer wiki for: design intent, glossaries, cross-cutting summaries, meeting decisions.

2. **L2 — Evidence (raw)**  
   - If the answer needs exact wording, numbers, or quotes: open files under `raw/` linked from page sources / summary backlinks.  
   - Do not treat wiki paraphrase as contractual truth.

3. **L3 — Code / external truth**  
   - Prefer `ruminate ask --agent` (planned): **rg + tree-sitter** (`symbol_search`, `read_enclosing`) — no gopls required.  
   - Optional: host **gopls MCP** when type-precise cross-package navigation is needed.  
   - **Do not** invent API behavior from wiki alone.

## Commands (CLI)

```bash
# Keyword search over wiki
ruminate find "query"

# Q&A
ruminate ask "question"
ruminate ask "question" --evidence auto   # planned: L1→L2 without multi-step
ruminate ask "question" --agent           # planned: ReAct (wiki/raw/code tools)
ruminate ask "question" -v                # retrieval / agent trace

# Code path (default in agent): rg + tree-sitter enclosing scope
# Optional LSP: gopls mcp — only if you need type-precise defs

# Health
ruminate lint
```

When MCP is available, prefer tools: `search_wiki`, `read_wiki_page`, `list_page_sources`, `read_raw` (see docs/109).

## Write-back

- Do **not** silently edit entity/concept pages during exploration.
- Only file new synthesis when the user asks, e.g. `ruminate ask "..." --save`, or after explicit confirmation.
- After durable architecture decisions in code review, suggest ingesting an ADR/note into the wiki.

## Anti-patterns

- Dumping the entire monorepo into chat when a wiki concept page already answers "why".
- Using only distilled wiki for "what is the default timeout" without checking config/code.
- Re-implementing a code agent inside ad-hoc scripts; use the host agent + LSP.
