# 搜索优化方案

## 当前搜索管线分析

### 现有架构

```
用户问题
  │
  ▼
hybridSearch(ctx, query, topN=5)
  │
  ├─ 1. 向量搜索: 取 topN*2 = 10 条
  │
  ├─ 2. FTS AND: 取 5 条 → RRF fuse(fts5条, vec10条) → 截断到 topN=5
  │     └─ 如果 AND 无结果:
  ├─ 3. FTS OR:  取 5 条 → RRF fuse(fts5条, vec10条) → 截断到 topN=5
  │     └─ 如果 OR 也无结果:
  └─ 4. 纯向量:  取 topN=5
```

`ask` 命令的 `--top-n` 默认值为 5（`internal/cmd/ask.go:80`），`find` 命令的 `--limit` 默认值为 20（`internal/cmd/find.go:79`）。

### 已识别的问题

#### 问题 1：5 条结果严重不足

一个原始 wiki 被 `ingest` 拆分成多个 pages，5 条结果几乎不可能覆盖需要跨页面信息的复杂问题。例如"Go 的 GC 是如何演进的"可能需要 `[[Go GC 原理]]`、`[[Go 1.5 GC 改进]]`、`[[Go GC 调优]]` 等多个 pages 的信息才能完整回答。此时如果只取 5 条，LLM 可能因为上下文不足而无法作答，用户会认为知识库缺少信息——但实际上信息是存在的。

**根本原因**：`ask` 的 `--top-n` 默认值 5 对于"给人看的搜索结果"是合理的（Google 首页也就 10 条），但对于"给 LLM 当上下文"来说严重不足。

#### 问题 2：FTS 结果作为独立候选项而非加权信号

当前 `rrfFuse`（`internal/wiki/manager.go:608-648`）中，FTS 结果和向量结果在 RRF 融合时地位完全平等：

```go
for i, r := range ftsResults {
    score[r.Path] += 1.0 / (k + float64(i+1))  // FTS rank #1 = 1/61 ≈ 0.0164
}
for i, r := range vecResults {
    score[r.Path] += 1.0 / (k + float64(i+1))  // vec rank #1 = 1/61 ≈ 0.0164
}
```

注释说 "FTS-only results are appended at the end for discovery"，但 RRF 融合并不会把 FTS-only 放到末尾——如果某个 page 只在 FTS 结果中出现且排名靠前（如 #1），它的 RRF 分数可能超过排在向量结果尾部的 page，从而**挤出**语义上更相关的内容。

**问题本质**：FTS 应该用于提升同时出现在向量结果中的 page 的权重（boosting），而不应该引入向量搜索完全没有命中的新候选项。FTS-only 的结果在语义上可能与 query 不相关（仅仅是关键词匹配），引入它会增加噪音。

#### 问题 3：缺少 Rerank 阶段

当前管线的阶段划分：

| 阶段 | 当前实现 | 问题 |
|------|---------|------|
| Recall（召回） | 向量取 10 条 + FTS 取 5 条 | 候选池太小 |
| Rerank（精排） | RRF 算术融合 | 不理解 query-document 语义关系 |
| Generate（生成） | LLM 基于 top-5 chunks | 上下文可能不完整 |

RRF 只是对两个排序列表做算术融合（`1/(k+rank)`），它不阅读文档内容，不理解 query 和 document 之间的语义关系。缺少一个真正的 rerank 步骤（cross-encoder 或 LLM 判分）来从候选池中筛选出真正相关的内容。

## 业界 RAG 检索最佳实践

### 多阶段级联检索架构

```
Query
  │
  ▼
┌──────────────────────────────────────────┐
│  Stage 1: Recall（高召回，低精度）         │
│                                          │
│  多路召回，每路取 20-50 条:               │
│  ├─ Dense (向量/embedding)                │
│  ├─ Sparse (BM25/关键词)                  │
│  ├─ 可选: 实体链接、知识图谱、HyDE        │
│  └─ 合并去重 → 100-200 candidates         │
│                                          │
│  目标: 宁可多，不能漏                       │
└──────────────────┬───────────────────────┘
                   ▼
┌──────────────────────────────────────────┐
│  Stage 2: Rerank（精排）                  │
│                                          │
│  对每个 candidate 计算 query-doc 相关性:   │
│  ├─ Cross-encoder (BGE-reranker,        │
│  │   Cohere Rerank, Jina Reranker)       │
│  ├─ 或 LLM-as-reranker                  │
│  └─ 取 top 5-20 送入生成阶段             │
│                                          │
│  目标: 从候选中挑出真正相关的               │
└──────────────────┬───────────────────────┘
                   ▼
┌──────────────────────────────────────────┐
│  Stage 3: Generate（生成）                │
│                                          │
│  LLM 基于 top-K chunks 生成答案           │
│  + 引用标注                               │
│                                          │
│  可选: 如果 LLM 判断信息不足，触发          │
│  二次检索 (iterative retrieval)            │
└──────────────────────────────────────────┘
```

### 核心洞察

检索和生成是不同的目标，不应混为一谈：

- **召回阶段**不求精准，但求全面。把可能有用的内容都找出来。这个阶段宁滥毋缺——候选多了只是多消耗一点 LLM 分析时间，候选少了丢失的是答案的完整性和准确性。
- **精排阶段**负责从候选池中筛选，把真正相关的内容排在前面。这是恢复精度的关键环节。
- **生成阶段**拿到高质量、够数量的上下文，自然能产出可信的答案。

### 业界实践参考

| 系统/公司 | 召回策略 | Rerank 方式 | 最终送入 LLM |
|-----------|---------|------------|-------------|
| Cohere | 多路 embedding + BM25 | Cross-encoder rerank | top 10-20 |
| OpenAI Assistants | embedding + keyword | 内置 rerank | 动态截断 |
| LangChain/LlamaIndex 最佳实践 | Hybrid (dense+sparse) | Cohere/Jina/ColBERT reranker | top 10-20 |
| GraphRAG (微软) | embedding + 知识图谱遍历 | LLM 自选 relevant entities | 动态 |
| RAGFlow | 多路召回 + 标签过滤 | Cross-encoder + LLM 二次筛选 | top 10 |

共同规律：

- 召回阶段取 50-200 条，各路独立取 20-50 条再合并去重
- **rerank 是标配**，不做 rerank 的 RAG 系统在 2024+ 已经很少见了
- 最终送入 LLM 的通常在 10-20 条，不是 5 条

## 改进方案

### 第一阶段：立即可做（低风险，高收益）

改动量小，不需要新依赖，可以立即改善实用性。

#### 1. 增大 `ask` 的 `--top-n` 默认值

```go
// 5 → 15
askCmd.Flags().IntVarP(&askTopN, "top-n", "n", 15, "Number of top search results to use as context")
```

即使没有 rerank，单纯增加候选数就能显著改善覆盖率。每个 page 截断到 4000 字符，15 篇也才约 60K tokens，现代模型完全能处理。token 消耗换取答案质量是划算的。

#### 2. 向量搜索取更多候选

```go
// topN*2 → topN*5，作为召回阶段的候选池
vecResults, _ := m.index.SearchByVector(queryVec, topN*5)
```

当 `topN=15` 时，向量召回 75 条，为 RRF 融合提供更充分的候选池。如果后续引入 rerank，这里的数量还可以进一步加大（如 `topN*10`）。

#### 3. FTS 改为纯 boosting 模式

当前 FTS 结果作为独立候选项参与 RRF 融合，可能引入噪音。改为：只对同时出现在向量结果中的 page 做 FTS 加权，不引入 FTS-only 的候选项。

```go
// 伪代码：FTS 只做加权，不做独立候选
vecSeen := makeSet(vectorPaths)
for _, r := range ftsResults {
    if vecSeen[r.Path] {
        score[r.Path] += boostWeight  // 只对向量已有的结果加权
    }
}
// 不引入 FTS-only 的新候选项
```

### 第二阶段：中等投入，显著收益

#### 4. 引入 Rerank 阶段

在召回和 LLM 生成之间加入 rerank 步骤：

```
Recall (100+ candidates) → Reranker → top 15-20 → LLM
```

**方案 A：本地 Reranker Model**

Ollama 生态中可用的小型 reranker，如 `BAAI/bge-reranker-v2-m3`（几百 MB），对 (query, doc) 对做联合编码，精度远超 cosine similarity。

**方案 B：LLM-as-Reranker**

使用同一个 LLM 对候选项做相关性打分。增加一次 LLM 调用，但比检索失败后用户重新提问的代价小。可以批量打分（一次调用评估多个 candidates）以降低延迟。

建议先在 `llm/` 包中抽象 `Reranker` 接口：

```go
type Reranker interface {
    Rerank(ctx context.Context, query string, docs []string) ([]ScoredDoc, error)
}
```

#### 5. Query Expansion / HyDE

在搜索前让 LLM 生成一个假设性答案，再用这个答案做 embedding 搜索。对概念性问题特别有效：

```
用户问: "Go GC 是如何演进的"
  ↓
LLM 生成假设答案: "Go 的 GC 从 1.0 的简单 mark-sweep 开始，
    到 1.5 引入并发标记，1.8 引入混合写屏障..."
  ↓
用假设答案做 embedding 搜索 → 召回的 pages 与假设答案语义更接近
```

**注意**：这需要额外的 LLM 调用，对响应延迟有影响。可以考虑作为可选项（`--hyde` flag），让用户在精度和速度之间选择。

### 第三阶段：需要更多设计

#### 6. Small-to-big / Parent Document Retrieval

当前每个 page 独立索引和检索，丢失了原始文档的上下文连贯性。改进方案：

- 索引时仍用当前 page 粒度（small chunk）
- 检索命中后，返回包含该 page 的更大上下文（上一个 + 当前 + 下一个 page，或整个 section）
- 这样 LLM 能获得更完整的上下文

实现思路：在索引中记录每个 page 的 `prev_page` 和 `next_page`（同属一个原始文档的相邻 pages），检索时按需扩展。

#### 7. Iterative Retrieval（迭代检索）

第一轮搜索 → LLM 生成初步答案 → LLM 识别信息缺口 → 针对缺口进行第二轮搜索 → 合并上下文 → 最终答案。

这解决了"5 条不够但 50 条太多"的矛盾，因为 LLM 自己驱动检索深度：

```
用户问: "Go GC 和 Java GC 的区别是什么"
  ↓
Round 1: 检索 → 找到 Go GC 相关 pages
  ↓
LLM: "我需要 Java GC 的信息来对比"
  ↓
Round 2: 检索 "Java GC" → 找到 Java GC 相关 pages
  ↓
LLM: 基于两轮结果生成完整对比答案
```

#### 8. 多路召回扩展

当前只有两路召回（向量 + FTS）。可以考虑扩展：

- **实体召回**：从 query 中提取实体（如 `[[Go]]`），通过 wiki 链接反向查找引用该实体的 pages
- **时间召回**：对时间敏感的问题（"最近的更新"），按修改时间过滤
- **标签/类别召回**：按 PageType（synthesis/raw/term）过滤

## 改进优先级总结

| 优先级 | 改进项 | 改动量 | 收益 | 风险 |
|--------|-------|--------|------|------|
| P0 | 增大 topN 默认值（5→15） | 1 行 | 高 | 极低 |
| P0 | 向量搜索取更多候选（*2→*5） | 1 行 | 高 | 极低 |
| P0 | FTS 改为纯 boosting | ~20 行 | 中 | 低 |
| P1 | 引入 Rerank 阶段 | 新模块 | 高 | 中 |
| P1 | Query Expansion / HyDE | 新功能 | 中高 | 中 |
| P2 | Small-to-big 检索 | 结构调整 | 中 | 中 |
| P2 | Iterative Retrieval | 架构改动 | 高 | 中高 |
| P3 | 多路召回扩展 | 增量添加 | 中 | 低 |

## 参考

- [Karpathy's LLM Wiki](llm_wiki.md) — 项目原始灵感
- [项目需求文档](requirements.md)
- [技术架构文档](architecture.md)
