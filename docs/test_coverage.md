# Test Coverage Report

> Generated: 2026-07-07 | Overall: **62.7%**

## Coverage by Package

| Package | Coverage | Status |
|---------|----------|--------|
| [internal/config](#internalconfig) | 92.6% | ✅ High |
| [internal/gitwrap](#internalgitwrap) | 89.2% | ✅ High |
| [internal/trace](#internaltrace) | 85.5% | ✅ High |
| [internal/lint](#internallint) | 81.2% | ✅ Good |
| [internal/llm](#internalllm) | 80.6% | ✅ Good |
| [internal/wiki](#internalwiki) | 64.5% | ⚠️ Medium |
| [internal/ingest](#internalingest) | 63.1% | ⚠️ Medium |
| [internal/query](#internalquery) | 41.8% | 🔴 Low |
| [internal/cmd](#internalcmd) | 18.2% | 🔴 Low |
| [internal/sync](#internalsync) | 12.3% | 🔴 Low |
| [cmd/ruminate](#cmdruminate) | 0.0% | ❌ None |

---

## Detailed Coverage by Function

### internal/config

| Function | Coverage | Notes |
|----------|----------|-------|
| `DefaultConfig` | 100.0% | |
| `Load` | 90.5% | |
| `ExpandPath` | 100.0% | |

### internal/gitwrap

| Function | Coverage | Notes |
|----------|----------|-------|
| `New` | 100.0% | |
| `Init` | 100.0% | |
| `Add` | 100.0% | |
| `AddAll` | 100.0% | |
| `Commit` | 100.0% | |
| `Log` | 87.5% | |
| `IsGitRepo` | 100.0% | |
| `RepoPath` | 100.0% | |
| `HeadSHA` | 75.0% | |
| `DiffNameStatus` | 78.6% | |
| `ListFiles` | 90.0% | |
| `run` | 100.0% | |
| `runOutput` | 83.3% | |
| `gitDir` | 100.0% | |
| `parseLogOutput` | 92.3% | |

### internal/trace

| Function | Coverage | Notes |
|----------|----------|-------|
| `New` | 100.0% | |
| `Enabled` | 66.7% | |
| `Begin` | 87.5% | |
| `End` | 81.8% | |
| `Error` | 83.3% | |
| `Root` | 0.0% | Missing |
| `Flush` | 83.3% | |
| `formatAttrs` | 87.5% | |
| `writeTree` | 90.9% | |
| `writeTreeNode` | 100.0% | |

### internal/lint

| Function | Coverage | Notes |
|----------|----------|-------|
| `AllChecks` | 100.0% | |
| `DefaultOptions` | 100.0% | |
| `New` | 100.0% | |
| `Run` | 97.6% | |
| `buildInlinkMap` | 100.0% | |
| `buildLinkTargetIndex` | 100.0% | |
| `checkOrphans` | 100.0% | |
| `checkBrokenLinks` | 88.9% | |
| `checkStaleness` | 90.9% | |
| `checkContradictions` | 88.2% | |
| `llmContradictionCheck` | 0.0% | Missing (needs LLM mock) |
| `findByPath` | 75.0% | |
| `sharedLinks` | 100.0% | |
| `truncateContent` | 100.0% | |

**Suppression:**

| Function | Coverage | Notes |
|----------|----------|-------|
| `suppressionFilePath` | 100.0% | |
| `LoadSuppressions` | 86.7% | |
| `Save` | 66.7% | |
| `Add` | 100.0% | |
| `Remove` | 100.0% | |
| `List` | 100.0% | |
| `IsSuppressed` | 100.0% | |
| `suppressionID` | 100.0% | |
| `FilterSuppressed` | 100.0% | |

### internal/llm

| Function | Coverage | Notes |
|----------|----------|-------|
| `NewProvider` | 100.0% | |
| `NewOllamaProvider` | 100.0% | |
| `Chat` | 83.3% | |
| `ChatStream` | 75.0% | |
| `toOllamaMessages` | 100.0% | |
| `NewEmbeddingProvider` | 100.0% | |
| `NewOllamaEmbedder` | 100.0% | |
| `Embed` | 83.3% | |
| `EmbedQuery` | 66.7% | |
| `Error` | 0.0% | Missing |

### internal/wiki

| Function | Coverage | Notes |
|----------|----------|-------|
| `NewIndexManager` | 100.0% | |
| `Init` (index) | 66.7% | |
| `createFTSTable` | 100.0% | |
| `createVectorTable` | 100.0% | |
| `serializeVector` | 100.0% | |
| `deserializeVector` | 100.0% | |
| `cosineSimilarity` | 100.0% | |
| `StoreVector` | 71.4% | |
| `DeleteVector` | 66.7% | |
| `SearchByVector` | 85.7% | |
| `searchByVectorWithMeta` | 79.2% | |
| `open` | 22.2% | Low |
| `AddPage` | 66.7% | |
| `AddRawSource` | 66.7% | |
| `UpdatePage` | 66.7% | |
| `RemovePage` | 66.7% | |
| `Search` | 78.6% | |
| `SearchWithSnippets` | 0.0% | Missing |
| `searchWithSnippets` | 0.0% | Missing |
| `toFTS5OrQuery` | 100.0% | |
| `splitForFTS5Query` | 100.0% | |
| `isCJK` | 100.0% | |
| `enrichContentWithBigrams` | 100.0% | |
| `CleanSnippet` | 100.0% | |
| `isBigramSoup` | 100.0% | |
| `extractCJKBigrams` | 100.0% | |
| `expandQueryCJKBigrams` | 88.9% | |
| `toFTS5AndQuery` | 88.9% | |
| `ReadIndexMd` | 0.0% | Missing |
| `Close` | 100.0% | |
| `ReindexContent` | 0.0% | Missing |
| `updateIndexMd` | 90.2% | |
| `rebuildIndexMd` | 73.1% | |
| `pageTypeSection` | 100.0% | |
| `parseIndexMd` | 100.0% | |

**Log:**

| Function | Coverage | Notes |
|----------|----------|-------|
| `NewLogManager` | 100.0% | |
| `Init` | 100.0% | |
| `Append` | 57.1% | Low |
| `ReadLog` | 83.3% | |
| `RecentEntries` | 83.3% | |
| `LogPath` | 0.0% | Missing |
| `formatEntry` | 100.0% | |
| `parseLogMd` | 85.0% | |

**Manager:**

| Function | Coverage | Notes |
|----------|----------|-------|
| `SetEmbeddingProvider` | 100.0% | |
| `SetTracer` | 0.0% | Missing |
| `NewManager` | 60.0% | |
| `Root` | 0.0% | Missing |
| `WikiDir` | 0.0% | Missing |
| `RawDir` | 0.0% | Missing |
| `Init` | 76.2% | |
| `IsInitialized` | 100.0% | |
| `AddSource` | 82.4% | |
| `RawSourcePath` | 0.0% | Missing |
| `ListSources` | 93.3% | |
| `Create` | 76.5% | |
| `Read` | 90.0% | |
| `ReadByPath` | 92.3% | |
| `Update` | 81.2% | |
| `Delete` | 80.0% | |
| `List` | 91.3% | |
| `Index` | 100.0% | |
| `Log` | 0.0% | Missing (getter) |
| `Git` | 0.0% | Missing (getter) |
| `Embedder` | 0.0% | Missing (getter) |
| `LLM` | 0.0% | Missing (getter) |
| `LLMConfig` | 0.0% | Missing (getter) |
| `Close` | 66.7% | |
| `Search` | 0.0% | **Missing - critical** |
| `hybridSearch` | 0.0% | **Missing - critical** |
| `ftsWithFallback` | 0.0% | Missing |
| `docList` | 0.0% | Missing |
| `scoredDocList` | 0.0% | Missing |
| `rrfFuse` | 100.0% | |
| `rrfFuseFull` | 0.0% | Missing |
| `computeAndStoreEmbedding` | 87.5% | |
| `deleteEmbedding` | 100.0% | |
| `ensureComponents` | 100.0% | |
| `sanitizeFilename` | 100.0% | |
| `Reindex` | 0.0% | Missing |

**Rerank & Expansion:**

| Function | Coverage | Notes |
|----------|----------|-------|
| `rerankWithLLM` | 95.0% | |
| `getContentPreview` | 11.1% | **Very low** |
| `buildRerankPrompt` | 90.9% | |
| `parseRerankResponse` | 92.9% | |
| `mmrDiversify` | 100.0% | |
| `expandQueries` | 94.4% | |
| `generateHypotheticalDoc` | 90.0% | |
| `multiQuerySearch` | 0.0% | Missing |
| `truncateForTrace` | 0.0% | Missing |

**Wikilink:**

| Function | Coverage | Notes |
|----------|----------|-------|
| `ParseWikiLinks` | 94.4% | |
| `ResolveWikiLink` | 100.0% | |
| `GenerateWikiLink` | 100.0% | |

### internal/ingest

| Function | Coverage | Notes |
|----------|----------|-------|
| `NewEngine` | 0.0% | **Missing - critical** |
| `IsSupportedExtension` | 0.0% | Missing |
| `Ingest` | 0.0% | **Missing - critical** |
| `analyze` | 0.0% | **Missing - critical** |
| `createSummaryPage` | 0.0% | Missing |
| `createEntityPages` | 0.0% | Missing |
| `createConceptPages` | 0.0% | Missing |
| `buildSummaryContent` | 97.2% | |
| `buildEntityContent` | 100.0% | |
| `buildConceptContent` | 100.0% | |
| `mergeEntityContent` | 100.0% | |
| `mergeConceptContent` | 100.0% | |
| `parseAnalysisResponse` | 81.2% | |
| `filepathToLink` | 100.0% | |
| `estimateTokens` | 100.0% | |
| `validateContentSize` | 100.0% | |

**Reader:**

| Function | Coverage | Notes |
|----------|----------|-------|
| `NewReader` | 100.0% | |
| `Register` | 100.0% | |
| `SupportedExtensions` | 100.0% | |
| `IsSupportedExtension` | 100.0% | |
| `Read` | 100.0% | |
| `readFile` | 100.0% | |
| `readURL` | 83.3% | |
| `extractTitle` | 95.0% | |
| `Error` | 0.0% | Missing |

### internal/query

| Function | Coverage | Notes |
|----------|----------|-------|
| `NewEngine` | 0.0% | Missing |
| `SetTracer` | 0.0% | Missing |
| `Find` | 0.0% | **Missing - critical** |
| `Ask` | 53.3% | Low |
| `AskStream` | 0.0% | **Missing** |
| `retrieveContext` | 88.9% | |
| `buildAskMessages` | 88.2% | |
| `saveToWiki` | 86.7% | |
| `truncate` | 100.0% | |
| `stripTags` | 100.0% | |
| `sourceDocList` | 84.6% | |

### internal/cmd

| Function | Coverage | Notes |
|----------|----------|-------|
| `init` (ask) | 100.0% | Cobra init only |
| `parseEffort` | 100.0% | |
| `runAskNonStream` | 0.0% | Missing |
| `runAskStream` | 0.0% | Missing |
| `init` (find) | 75.0% | |
| `ansiHighlight` | 100.0% | |
| `plainHighlight` | 100.0% | |
| `init` (hook) | 100.0% | |
| `init` (ingest) | 100.0% | |
| `init` (init) | 100.0% | |
| `init` (lint) | 100.0% | |
| `printLintReport` | 0.0% | Missing |
| `severityIcon` | 100.0% | |
| `checkLabel` | 100.0% | |
| `wrapLines` | 100.0% | |
| `init` (reindex) | 100.0% | |
| `Execute` | 0.0% | Missing |
| `init` (root) | 100.0% | |
| `exitWithError` | 0.0% | Missing |
| `init` (sync) | 100.0% | |
| `loadConfig` | 0.0% | Missing |
| `mergeVerbose` | 0.0% | Missing |
| `printConfig` | 0.0% | Missing |

### internal/sync

| Function | Coverage | Notes |
|----------|----------|-------|
| `NewEngine` | 0.0% | **Missing** |
| `statePath` | 100.0% | |
| `loadState` | 81.8% | |
| `saveState` | 66.7% | |
| `Sync` | 0.0% | **Missing - critical** |
| `processChanges` | 0.0% | **Missing - critical** |
| `ingestFile` | 0.0% | Missing |
| `handleDeletedFile` | 0.0% | Missing |

### cmd/ruminate

| Function | Coverage | Notes |
|----------|----------|-------|
| `main` | 0.0% | Entry point, typically not unit-tested |

---

## Gap Analysis & Improvement Plan

### Priority 1 — Critical (0% on main flows)

| Package | Functions | Effort | Approach |
|---------|-----------|--------|----------|
| sync | `NewEngine`, `Sync`, `processChanges`, `ingestFile`, `handleDeletedFile` | Medium | Mock git + LLM, test state machine transitions |
| ingest | `NewEngine`, `Ingest`, `analyze` | Medium | Mock LLM response, verify page generation |
| query | `NewEngine`, `Find`, `AskStream` | Medium | Mock LLM + index, test search/ask flows |
| wiki | `Search`, `hybridSearch`, `ftsWithFallback` | Medium | Need index fixture, mock embedder |

### Priority 2 — Low Coverage (< 60%)

| Function | Current | Target | Approach |
|----------|---------|--------|----------|
| `asker.Ask` | 53.3% | 80%+ | Test more prompt variations |
| `manager.NewManager` | 60.0% | 80%+ | Test error paths |
| `log.Append` | 57.1% | 80%+ | Test edge cases (duplicates, formatting) |
| `index.open` | 22.2% | 80%+ | Test with real temp DB |
| `rerank.getContentPreview` | 11.1% | 80%+ | Test various content lengths |

### Priority 3 — Low Hanging Fruit

| Package | Functions | Notes |
|---------|-----------|-------|
| trace | `Root` | Simple getter, trivial to test |
| wiki/manager | `Root`, `WikiDir`, `RawDir`, `Log`, `Git`, `Embedder`, `LLM`, `LLMConfig` | Simple getters |
| cmd | `loadConfig`, `mergeVerbose`, `printConfig` | Pure functions, easy to test |
| cmd | `printLintReport` | Table-driven, test output format |
| cmd | `exitWithError` | Test os.Exit behavior |
| lint | `llmContradictionCheck` | Needs LLM mock |

---

## Tracking

| Date | Overall | Change | Notes |
|------|---------|--------|-------|
| 2026-07-07 | 62.7% | — | Initial baseline |
