# 搜索优化方案

## 当前搜索管线

### 架构

```
用户问题
  │
  ▼
hybridSearch(ctx, query, topN)
  │
  ├─ 1. 向量搜索: 固定召回 200 条候选（含 embedding vector + cosine score）
  │
  ├─ 2. FTS AND: 取 5 条 → RRF boost 向量结果中的 FTS 命中项（纯加权，不引入 FTS-only）
  │     └─ 如果 AND 无结果 → FTS OR: 取 5 条 → RRF boost
  │
  ├─ 3. MMR 多样化 (λ=0.5, target=50): 从融合后的 pool 中迭代选出既相关又多样的 50 条
  │
  ├─ 4. LLM Rerank: 使用 LLM 对 MMR 结果做 listwise 重排序，提升精度
  │     └─ 自动启用（LLM provider 可用时）；出错时 fallback 到 MMR 原始排序
  │
  └─ 5. 截断到 topN 条结果给 LLM
```

### 参数分层

| 参数 | 控制什么 | 默认值 | 说明 |
| ---- | -------- | ------ | ---- |
| 召回池 | 向量搜索候选数量 | 200 | 固定，不受任何外部参数影响 |
| MMR target | MMR 多样化选出数量 | 50 | 固定，保证足够轮次让多样性惩罚积累 |
| `--top-n` | 喂给 LLM 的最终数量 | 20 | 只控制 LLM 上下文预算，不影响搜索质量 |

三者彻底解耦——`--top-n` 调大调小只影响 LLM 收到的上下文量，不影响搜索和多样化的质量。

### 核心设计决策

#### 召回池与 topN 解耦

- **召回池**：固定在 200 条，不受 `--top-n` 影响。够大才能覆盖多语义簇——即使 GC 相关内容有 100+ 条排在 THP 前面，200 条的池子也能把 THP 内容兜进来
- **`--top-n`**（默认 20）：只控制最终喂给 LLM 的上下文数量，是 LLM 的 token 预算

#### MMR target 与 topN 解耦

MMR 需要足够的迭代轮次让多样性惩罚积累——前几轮会选 dominant cluster（如 GC）的最相关结果，随着该 cluster 的多样性惩罚增大，minority cluster（如 THP）开始被选中。如果 MMR 只选 20 轮，可能还不够把 THP 选出来就结束了。固定选 50 轮确保多样性生效，然后截断到 `--top-n` 条返回。

#### FTS 纯加权（boosting）

FTS 结果不作为独立候选项参与 RRF 融合。只对同时出现在向量结果中的 page 做加权，不引入 FTS-only 的候选项——关键词匹配本身不代表语义相关。

```go
// rrfFuse / rrfFuseFull: 只对向量已有的结果加权
vecPaths := make(map[string]bool)
for _, r := range vecResults {
    vecPaths[r.Path] = true
}
for i, r := range ftsResults {
    if vecPaths[r.Path] {
        score[r.Path] += 1.0 / (k + float64(i+1)) // 加权，不新增
    }
}
```

#### RRF（Reciprocal Rank Fusion）

RRF 是一种融合多个排序列表的经典算法，用于将不同来源的搜索结果（向量搜索、FTS、多查询变体）合并为统一的排序。

**公式**：

```
RRF_score(d) = Σ 1 / (k + rank_i(d))
```

- `k` 是常数（通常取 60），防止除以零并平滑高排名项的影响
- `rank_i(d)` 是文档 `d` 在第 `i` 个排序列表中的排名（1-based）
- 文档在多个列表中出现时，各路的 RRF 分值累加

**为什么用 RRF 而不是分数直接加权**：

不同搜索来源的原始分数尺度不统一（cosine similarity 在 [0,1]，BM25 无上界），直接加权需要归一化且对异常值敏感。RRF 只关心排名，天然规避了跨来源分数尺度不一致的问题。

**在当前管线中的用途**：

| 位置 | 函数 | 用途 |
|------|------|------|
| [manager.go:790](internal/wiki/manager.go#L790) | `rrfFuseFull` | FTS 结果作为 booster 融入向量搜索结果 |
| [expansion.go:191](internal/wiki/expansion.go#L191) | `rrfFuseMultiQuery` | 多个 query 变体的向量搜索结果融合 |

**直观示例**（多查询融合）：

| 排名 | q1 结果 | q2 结果 | q3 结果 |
|------|---------|---------|---------|
| 1 | Doc A | Doc A | Doc B |
| 2 | Doc B | Doc C | Doc D |

Doc A 在 q1 和 q2 中都排第 1，RRF 分数最高（两路命中）；Doc B 在 q1 排第 2、q3 排第 1，次之。最终排序：A > B > C > D。

#### MMR 多样化

MMR (Maximal Marginal Relevance) 迭代选择既与 query 相关、又与已选结果不相似的结果：

```
MMR = argmax [ λ·cosineSim(d, query) - (1-λ)·max cosineSim(d, already_selected) ]
```

- **λ=0.5**：相关性和多样性各占一半。比 0.7 更激进地引入不同语义簇的内容
- **MMR target=50**：独立于 `--top-n`，保证足够迭代轮次
- 复用已有的 embedding vectors 计算 document-document 相似度，无需额外开销
- 对于 "Go GC 如何适应透明巨页" 这类跨领域查询，GC 内容被大量选中后，后续轮次 MMR 会自动倾向 THP 内容

## 已识别的问题与解决方案

### 问题 1：初始结果太少 ✅ 已修复

- `ask` 的 `--top-n` 默认值从 5 提升到 20
- 每个 page 截断到 4000 字符，20 篇约 80K chars
- `AskOptions.TopN` 默认值使用常量 `DefaultTopN = 20`

### 问题 2：FTS 结果作为独立候选项引入噪音 ✅ 已修复

FTS 改为纯 boosting 模式。`rrfFuse` / `rrfFuseFull` 中 FTS 只对已存在于向量结果中的 page 加权，不引入 FTS-only 候选项。

### 问题 3：缺少多样化机制 ✅ 已修复

引入 MMR 多样化环节。从 200 条候选池中选出 50 条多样化结果，再截断到 `--top-n` 条。

### 问题 5：缺少 Rerank 精排 ✅ 已修复

MMR 保证了多样性，但 RRF 分数（cosine + keyword boost 的融合）对精细相关性的判断不够准确。引入 LLM-based listwise rerank 在 MMR 之后、截断之前对 50 条候选做重排序。

**Rerank 策略**：
- **输入**：MMR 选出的 ~50 条多样化候选
- **方式**：单次 LLM Chat 调用，将所有候选（标题 + 内容摘要）发送给 LLM，让 LLM 按相关性排序
- **输出格式**：`{"ranked_ids": [3, 1, 5, 2, 4]}` — 1-based 文档 ID，按相关性降序
- **自动启用**：当 LLM provider 配置可用时自动启用；无 LLM 或调用失败时 fallback 到 MMR 原始排序
- **跳过条件**：候选数 ≤ topN 时跳过（全部返回无需重排）

**与 MMR 的互补关系**：
- MMR：确保结果覆盖不同的语义簇（GC、THP、内存管理等）
- Rerank：在每个簇内和跨簇间选出最精准相关的条目排在前面

```
Vector search (200) → FTS boosting (RRF) → MMR diversity (50) → LLM Rerank → Truncate (topN)
```

### 问题 6：Query Expansion / HyDE ✅ 已实现

用户提问方式与文档写法之间存在词汇鸿沟（"透明巨页" vs "THP"）和语体鸿沟（疑问句 vs 陈述句）。引入 Query Expansion 和 HyDE 两种查询改写技术，通过 `--effort` flag 控制：

**Effort 级别**：

| Level | 技术 | LLM 调用 | 延迟 | 适用场景 |
|-------|------|---------|------|---------|
| `fast` | 无（直接搜索） | 0 | 最低 | 术语匹配良好、对延迟敏感 |
| `balanced` | Query Expansion | 1次 | 中 | 用户用词与文档术语不一致 |
| `thorough` | HyDE | 1次 | 较高 | 问题是疑问句、文档是陈述句 |

**Query Expansion (balanced)**：
- LLM 将原始 query 改写为 2-3 个不同角度的变体
- 每个变体独立做向量搜索，结果通过 RRF 融合
- 原始 query 始终参与搜索，确保不丢失原始意图

**HyDE (thorough)**：
- LLM 生成一篇假设答案文档（Hypothetical Document）
- 用假设文档的 embedding 代替原始 query embedding 做向量搜索
- 假设文档的陈述风格 embedding 更接近真实文档的分布

**降级策略**：
- LLM provider 不可用时：自动降级到 `fast`
- expansion/HyDE 调用失败时：fallback 到原始 query 的搜索结果
- 与 rerank 一致：best-effort，不阻塞搜索

**使用方式**：
```bash
ruminate ask --effort fast "Go GC 如何适应透明巨页"      # 基准行为
ruminate ask --effort balanced "Go GC 如何适应透明巨页"   # 查询扩展
ruminate ask --effort thorough "Go GC 如何适应透明巨页"   # HyDE
```

```
[effort=balanced]  Query → LLM expand → [q1,q2,q3] → 3×vector search → RRF merge → FTS boost → MMR → Rerank → topN
[effort=thorough]  Query → LLM hypo doc → embed(hypo) → vector search → FTS boost → MMR → Rerank → topN
[effort=fast]      Query → embed(query) → vector search → FTS boost → MMR → Rerank → topN
```

### 问题 4：召回池太小，且与 topN 耦合 ✅ 已修复

- 向量召回从 `topN*2` 改为固定 200 条（与 topN 解耦）
- MMR target 固定 50（与 topN 解耦）
- `--top-n` 只控制最终返回数量

## 改进优先级总结

| 优先级 | 改进项 | 状态 |
| ------ | ------ | ---- |
| P0 | 增大 topN 默认值（5→20） | ✅ |
| P0 | 召回池固定 200（与 topN 解耦） | ✅ |
| P0 | MMR target 固定 50（与 topN 解耦） | ✅ |
| P0 | FTS 改为纯 boosting | ✅ |
| P0 | MMR 多样化（λ=0.5） | ✅ |
| P1 | 引入 Rerank 阶段 | ✅ |
| P1 | Query Expansion / HyDE | ✅ 已实现（通过 `--effort` 控制） |
| P2 | Small-to-big 检索 | 待定 |
| P2 | Iterative Retrieval | 待定 |
| P3 | 多路召回扩展 | 待定 |

## 文件变更记录

| 文件 | 改动说明 |
| ---- | -------- |
| `internal/cmd/ask.go` | 新增 `--effort` flag，默认 `fast`；新增 `parseEffort` |
| `internal/cmd/find.go` | `Search` 调用传入 `SearchEffortFast` |
| `internal/query/asker.go` | `AskOptions` 新增 `Effort` 字段；`retrieveContext` 透传 effort |
| `internal/wiki/expansion.go` | **新文件** — Query Expansion、HyDE、RRF 多查询融合实现 |
| `internal/wiki/expansion_test.go` | **新文件** — 15 个 expansion/HyDE 测试用例 |
| `internal/wiki/manager.go` | `Search`/`hybridSearch` 方法签名新增 `effort` 参数；hybridSearch 集成 expansion/HyDE |
| `docs/search-optimization.md` | 查询扩展状态更新，补充 effort level 文档 |

## 参考

- [Karpathy's LLM Wiki](llm_wiki.md) — 项目原始灵感
- [项目需求文档](requirements.md)
- [技术架构文档](architecture.md)
