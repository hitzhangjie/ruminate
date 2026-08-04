# Karpathy LLM Wiki 的 query 留白与 Ruminate 的补全

> 记录对 Karpathy 原文中 raw sources 在查询阶段角色缺失的分析，以及 Ruminate 双真相模型如何补上这一环。
> 最后更新：2026-08-04

---

## 一、Karpathy 原文的留白

重读 [llm_wiki.md](../llm_wiki.md)，三层架构的表述是：

| 层 | 角色 |
|----|------|
| Raw sources | 只读，LLM 从中读取但绝不修改。"This is your source of truth." |
| The wiki | LLM 生成和维护的 markdown，夹在人与 raw 之间 |
| The schema | 约定 wiki 结构和工作流 |

操作层面：

- **Ingest**：读 raw → 提取 → 写入/更新 wiki pages
- **Query**："The LLM searches for relevant pages, reads them, and synthesizes an answer"——此处的 "pages" 指 wiki pages

**原文没有明确描述 raw sources 在 query 阶段的参与路径。** 给人的感觉是：

```text
raw sources ──ingest──► wiki pages ──query──► answer
  (只进不出)            (唯一查询面)
```

raw sources 的定位更像是 **wiki 的构建原料 + 未来 rebuild 的起点**，查询时不派上用场。

## 二、为什么这可能是问题

1. **蒸馏有损**：ingest 过程经过 LLM 压缩，丢弃的信息可能是某次查询的关键细节
2. **"source of truth" 名不副实**：如果查询路径永远不触及 raw，那 raw 只是备份，不是 truth
3. **规模假设**：Karpathy 假设 ~100 sources、~hundreds of pages，蒸馏损失可控；企业/代码场景下损失不可接受

## 三、Karpathy 为什么没强调这层（推测）

- 他的核心洞察是 **bookkeeping 自动化**，不是检索完整性
- 个人 wiki 场景下，wiki pages 本身密度足够，回退 raw 的需求不强烈
- ~100 sources 规模下，即使偶尔需要，手动翻 raw 也够用

## 四、Ruminate 的补全

[108-dual-truth-and-layered-retrieval.md](108-dual-truth-and-layered-retrieval.md) 确立的模型：

### 4.1 双真相

| 层 | 名称 | 查询角色 |
|----|------|----------|
| Evidence | raw + 外部源码/文档 | 事实锚点，可引用、可验证 |
| Synthesis | wiki pages | 高密度编译视图，默认查询入口 |

### 4.2 分层回退

```text
用户问题
  │
  ├─ L1  Wiki（Synthesis）  ← 默认，Karpathy 方案止于此
  │     不足则 ↓
  ├─ L2  Raw Evidence       ← Ruminate 补上的关键一环
  │     不足则 ↓
  └─ L3  External Truth     ← 源码、LSP、外部系统
```

下沉触发条件：LLM 自评 confidence 低、检索结果少、问题含"原文怎么说"等触发词、用户显式要求。

### 4.3 Key enabler：contributing sources

每个 wiki page 的 frontmatter 记录 `sources` 字段，使 L1→L2 下沉时可以**精准打开关联 raw**，无需全库盲搜：

```yaml
---
sources:
  - path: raw/article/go-gc-overview.md
    ingested_at: 2026-07-02
---
```

## 五、一句话总结

> Karpathy 说 raw 是 "source of truth"，但没有给 query 路径打通到 raw 的协议。Ruminate 用 **双真相 + 分层回退 + contributing sources** 补上了这个协议——让 raw 从"备份"变成"可回退的证据"。

## 六、相关文档

- [108-dual-truth-and-layered-retrieval.md](108-dual-truth-and-layered-retrieval.md) — 双真相模型与分层召回
- [109-agent-exploration.md](109-agent-exploration.md) — ReAct 探索与代码理解
- [101-wiki-maintenance-model.md](101-wiki-maintenance-model.md) — wiki 维护模型
- [llm_wiki.md](../llm_wiki.md) — Karpathy 原文
