# Iterative Retrieval（迭代检索）

## 概述

Iterative retrieval 是一种多轮检索策略：检索 → 评估 → 优化查询 → 再检索，循环直到获得满意结果。
与单轮检索（single-shot retrieval）不同，它通过多轮尝试逐步逼近目标，而非一次检索定胜负。

## 两种执行模式

### Eager（并行多路召回）

所有查询变体**同时**发出，合并结果后一起处理。不判断"够不够"，直接上全部手段。

```
Query → [原始query向量搜索] ──┐
       [expansion变体1搜索]  ──┼── RRF融合 → FTS boost → MMR → Rerank → topN
       [expansion变体2搜索]  ──┤
       [expansion变体3搜索]  ──┘
```

- **延迟**：取决于最慢的那一路，但各路并行，总延迟接近单次搜索
- **资源**：一次性消耗多路 LLM + 检索开销
- **质量**：多角度结果一次性汇聚，覆盖面广

### Incremental（增量式/迭代式）

先试原 query，评估结果质量，不够再加一轮（expansion / HyDE / 改写），逐轮追加。

```
Query → 搜索 → 评估(够不够?) ──yes──→ 返回
                    │
                   no
                    │
                    ▼
              改写query → 搜索 → 追加结果 → 评估(够不够?) ──...
```

- **延迟**：逐轮串行，延迟随轮次累加
- **资源**：按需触发，简单 query 更省
- **质量**：可能过早"满足"于第一轮结果，错过互补视角

## Ruminate 的选择：Eager 模式

当前 ruminate 的 `--effort` 参数控制的是**用多少种召回手段**，而非"什么时候该停"：

| Effort | 策略 | 执行方式 |
|--------|------|---------|
| `fast` | 原始 query | 单路 |
| `balanced` | Query Expansion (2-3 variants) + 原始 query | 并行多路，RRF 融合 |
| `thorough` | HyDE (假设文档) | 单路，但 embedding 来自 LLM 生成的假设答案 |

### 为什么选 eager 而非 incremental

1. **延迟敏感**：知识库搜索场景下，用户不会等。各路并行检索，总延迟接近单次搜索时间，而非逐轮累加。

2. **判断"够不够"本身就是个难题**：
   - 召回数阈值？不同 query 的期望返回量差异巨大
   - 相似度阈值？cosine 分数的绝对值受 embedding 模型影响，阈值难调
   - 相关性评估？需要再调一次 LLM，增加延迟和成本
   - 阈值设不好反而误事——过早停止遗漏关键结果，或者永远不满足一直循环

3. **知识库的召回量本就有限**：不像网页搜索有几百万结果。很多时候原 query 召回就不多，expansion 和 HyDE 是雪中送炭而非锦上添花，一开始就用更合理。

4. **本质上是多路召回融合**：虽然多轮改写符合"迭代检索"的定义，但执行上是并行的。这是刻意为之——用并行的方式获取迭代检索的质量收益，同时避开串行的延迟惩罚。

### 与其他搜索阶段的关系

当前搜索管线中，各类策略各司其职：

```
Query
  │
  ├─ Iterative Retrieval（多路改写 + 并行召回）
  │   └─ Query Expansion / HyDE — 解决词汇鸿沟和语体鸿沟
  │
  ├─ Hybrid Search（向量 + FTS 融合）
  │   └─ RRF fusion + FTS boosting — 结合语义和关键词匹配
  │
  ├─ Diversification（多样性）
  │   └─ MMR (λ=0.5) — 确保不同语义簇的结果都有机会出现
  │
  ├─ Precision（精排）
  │   └─ LLM Rerank — 在多样化候选池中选出最精准相关的条目
  │
  └─ Truncation
      └─ 截断到 topN，控制 LLM 上下文预算
```

各阶段解决的问题正交：
- **Iterative Retrieval** 解决"没找到"（recall）
- **Hybrid Search** 解决"找不全"（recall）
- **MMR** 解决"全是一种"（diversity）
- **Rerank** 解决"不够准"（precision）
- **Truncation** 解决"塞不下"（context budget）

## 总结

Ruminate 的 iterative retrieval 已经通过 Query Expansion 和 HyDE 实现，采用 eager 并行执行策略。
这是一种务实的工程选择：在延迟、资源、质量三者之间，优先保证延迟和质量，接受适度的资源开销。
对于本地知识库这种规模的搜索场景，这是合理的取舍。

## 参考

- [搜索优化方案](104-search-optimization.md) — 完整搜索管线文档
- [Small-to-Big 检索](106-small-to-big-retrieval.md) — 小粒度索引、大上下文返回
- [Ingest 与 Lint 职责分离](100-ingest-lint-separation.md) — Ingest 与 Lint 职责分离
- [Wiki 维护模型](101-wiki-maintenance-model.md) — raw 为真相源 vs wiki 为真相源
