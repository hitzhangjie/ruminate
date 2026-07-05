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
| P1 | Query Expansion / HyDE | 待定 |
| P2 | Small-to-big 检索 | 待定 |
| P2 | Iterative Retrieval | 待定 |
| P3 | 多路召回扩展 | 待定 |

## 文件变更记录

| 文件 | 改动说明 |
| ---- | -------- |
| `internal/cmd/ask.go` | `--top-n` 默认值 5→20，描述更新为 LLM context |
| `internal/query/asker.go` | 新增 `DefaultTopN = 20` 常量；`Ask`/`AskStream` 使用常量作为 fallback |
| `internal/wiki/index.go` | 新增 `scoredResult` 内部类型；新增 `searchByVectorWithMeta` 方法 |
| `internal/wiki/manager.go` | 改写 `hybridSearch`：固定召回 200、MMR target 50、截断到 topN；`rrfFuse` 保持纯 boosting；新增 `rrfFuseFull` |
| `internal/wiki/mmr.go` | **新文件** — MMR 多样化算法实现 |
| `internal/wiki/mmr_test.go` | **新文件** — 6 个 MMR 测试用例 |
| `internal/wiki/rerank.go` | **新文件** — LLM listwise rerank 实现 |
| `internal/wiki/rerank_test.go` | **新文件** — 10 个 rerank 测试用例 |
| `internal/wiki/search_test.go` | 更新 3 个 RRF 测试用例匹配新语义 |
| `docs/search-optimization.md` | 搜索优化方案文档 |

## 参考

- [Karpathy's LLM Wiki](llm_wiki.md) — 项目原始灵感
- [项目需求文档](requirements.md)
- [技术架构文档](architecture.md)
