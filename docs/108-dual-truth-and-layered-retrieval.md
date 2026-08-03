# 双真相模型与分层召回

> 本文档确立 Ruminate 对「编译型 Wiki」与「原文/源码证据层」的关系，以及查询时的分层回退策略。
> 最后更新：2026-08-04
>
> 背景讨论见会话 review：提炼即有损压缩；企业场景必须能回到证据。

---

## 一、问题陈述

Karpathy 的 LLM Wiki 核心是：**编译一次、持续保鲜**——用 Wiki 夹在人与原始资料之间，避免每次查询都从 raw 重新拼装。

这解决了 **bookkeeping**，但带来一个不可回避的权衡：

```text
原文 / 代码  ──① 抽取/清洗──►  中间文档  ──② ingest 蒸馏──►  Wiki
                 有损                      有损
```

- ① 代码→文档：丢实现细节、边界条件、版本差异
- ② 文档→entity/concept/summary：LLM 过早丢弃它认为不重要的信息

若 **查询路径只读 Wiki**，被丢掉的信息可能永远回不来——尽管 raw 文件仍躺在磁盘上。

当前实现的相关事实：

| 能力 | 状态 |
|------|------|
| ingest 时归档到 `raw/` | ✅ |
| summary 链回 raw 路径 | ✅（人可点） |
| raw 进入 FTS / 向量索引 | ❌（已移除，见 commit `79b21ed`） |
| ask 分层：Wiki 不足 → 读 raw | ❌ |
| 贡献源 frontmatter | ❌ |
| 代码/符号级回退 | ❌ |

**结论**：存储上 raw 存在；**查询协议上 raw 几乎不可达**。本文档补上协议与原则。

---

## 二、双真相原则

### 2.1 定义

| 层 | 名称 | 角色 | 可变性 |
|----|------|------|--------|
| **Evidence** | 事实真相 | raw 归档、外部源仓库、代码树、权威文档 | 不可变快照 / 或外部真相系统 |
| **Synthesis** | 理解真相 | wiki 页面（summary / entity / concept / synthesis） | LLM 维护、可 lint 修复 |

- **Evidence** 回答：「原始材料实际写了什么？代码实际怎么实现的？」
- **Synthesis** 回答：「综合多源之后，我们当前理解是什么？矛盾点在哪？」

Wiki **不是** Evidence 的替代品，而是 Evidence 之上的 **编译视图**。

### 2.2 与 Karpathy 的关系

| Karpathy | Ruminate 双真相 |
|----------|-----------------|
| Raw sources are source of truth | Evidence = 事实真相（保留并强化） |
| Wiki sits between you and raw | Synthesis = 高密度中间层（保留） |
| Query against the wiki | 默认先查 Synthesis；不够则 **escalation 到 Evidence** |
| index 导航即可 | 中小规模仍可；企业需工具化回退 |

个人研究场景可弱化 Evidence 回退；**企业 / 代码 / 合规场景必须强化**。

### 2.3 回答的诚实性

生成答案时必须能区分：

1. **来自 Synthesis**（综合结论，可能已过时或蒸馏有损）
2. **来自 Evidence**（可引用路径、片段、符号位置）
3. **证据不足** → 明确说不知道，而不是编造

禁止：仅用 Synthesis 编造「看起来自洽」的事实细节（尤其是 API 行为、配置默认值、法律/金额/日期）。

---

## 三、分层召回（L1 → L2 → L3）

与 [105-iterative-retrieval.md](105-iterative-retrieval.md) 中的 **eager 多路召回**正交：

- Eager / expansion / HyDE / MMR / rerank：解决 **同一层内的 recall / precision**
- 分层 escalation：解决 **跨层证据下沉**

```text
用户问题
  │
  ├─ L1  Wiki（Synthesis）
  │      向量 + FTS + expansion/HyDE + MMR + rerank
  │      用途：综合理解、交叉引用、快速回答
  │      触发下沉：证据不足 / 矛盾 / 需精确引用 / 用户要求原文
  │
  ├─ L2  Raw Evidence
  │      按 contributing sources 打开 raw；或对 raw/ 做 FTS/grep
  │      用途：核对摘要、补回蒸馏丢失的细节
  │
  └─ L3  External Truth（企业/代码）
         源码树、LSP 符号、测试、外部 wiki、工单系统…
         用途：定义查找、实现行为、未入库的活知识
```

### 3.1 默认策略（产品行为）

| 模式 | CLI 示意 | 行为 |
|------|----------|------|
| `wiki`（默认，兼容现状） | `ruminate ask "..."` | 仅 L1，单轮 RAG |
| `auto` | `ruminate ask "..." --evidence auto` | L1 → 不足则 L2 |
| `raw` | `ruminate ask "..." --evidence raw` | L1+L2 同时或强制带 raw 片段 |
| `agent`（P1） | `ruminate ask "..." --agent` | **ReAct**；wiki/raw + rg/tree-sitter；gopls 可选 |

「不足」的判定（启发式，可迭代）：

- LLM 自评 / 结构化字段：`confidence`、`missing`
- 检索结果过少或 rerank 后相关分过低
- 问题含「原文怎么说」「接口签名」「默认值」「源码」等触发词
- 用户显式要求引用 Evidence

### 3.2 与维护模型的关系

见 [101-wiki-maintenance-model.md](101-wiki-maintenance-model.md)：

- **理解/结构问题** → 修 Wiki（Synthesis）
- **事实错误** → 优先修 Evidence 源，再 re-ingest / rebuild 受影响页
- 仅改 Wiki 不回源 → 允许，但必须可标注 confidence / stale，且不得消灭 Evidence 链接

---

## 四、数据面要求（支撑分层）

### 4.1 Contributing sources（P0 数据契约）

每个 wiki page frontmatter 应能回答：「这段理解从哪来？」

```yaml
---
title: 垃圾回收
type: concept
sources:
  - path: raw/article/go-gc-overview.md
    ingested_at: 2026-07-02
  - path: raw/note/gc-meeting.md
    ingested_at: 2026-07-10
---
```

可选增强（P1）：

```yaml
sources:
  - path: raw/article/go-gc-overview.md
    spans:          # 可选：原文段落锚点
      - heading: "Tri-color mark"
    repo: null      # 若来自 sync 的外部仓库
```

用途：

- L1 命中 concept 后，agent/`--evidence auto` 可 **直接打开关联 raw**，无需全库盲搜
- lint 提示「事实过时可能来自哪些源」
- rebuild 时可按源重放

### 4.2 Raw 的可检索性（策略选择）

历史上 raw 曾进入 FTS，后因噪声/与 wiki 混排被移除。分层模型下建议：

| 选项 | 说明 | 推荐 |
|------|------|------|
| A. raw 单独索引 `raw_fts` | 不与 wiki 混排；仅 L2 查询 | **推荐** |
| B. 统一索引 + `layer` 字段过滤 | 实现简单，查询时 `layer=wiki\|raw` | 可接受 |
| C. 不索引，仅 path 打开 | 依赖 contributing sources + 全文件读 | 规模小时够用 |

**禁止**再回到「raw 与 wiki 混在一个排序列表里抢 top-N」而不标注 layer——那会冲淡 Synthesis 的密度优势。

### 4.3 rebuild

`ruminate rebuild`（见任务 6.1）从 Evidence 重放 Synthesis，是对抗「蒸馏偏差永久沉积」的安全阀。分层召回不能替代 rebuild，但能降低「答案永远缺一块」的用户痛苦。

---

## 五、与检索管线文档的衔接

| 文档 | 关系 |
|------|------|
| [104-search-optimization.md](104-search-optimization.md) | L1 内 hybrid / MMR / rerank 不变 |
| [105-iterative-retrieval.md](105-iterative-retrieval.md) | L1 内 eager 多路；分层是跨层 |
| [106-small-to-big-retrieval.md](106-small-to-big-retrieval.md) | 「召回 page 后回填 raw」正式上升为 L1→L2 协议 |
| [109-agent-exploration.md](109-agent-exploration.md) | L1–L3 的工具循环与代码理解 |

---

## 六、分阶段落地

### Phase A — 原则与数据（文档 + 轻量实现）

1. 确立本文与 101 的双真相表述
2. ingest 写入 `sources` frontmatter
3. `ask --evidence auto|raw|wiki`：auto 时对命中页的 sources 读入 raw 片段

### Phase B — Raw 独立检索

1. `raw_fts` 或带 layer 过滤的索引
2. L2 专用检索入口，不污染默认 L1 top-N

### Phase C — ReAct 探索

1. 见 [109-agent-exploration.md](109-agent-exploration.md)
2. **内嵌 ReAct**（wiki/raw + **rg + tree-sitter**，含 enclosing；gopls 可选）
3. 对外 MCP/skills 与内嵌路径并存

---

## 七、非目标（当前明确不做）

- 不把 Ruminate 做成通用 IDE / 代码智能体平台
- 不在 P0 实现完整 LSP 主机
- 不要求用户双写维护 raw 与 wiki 的每一处文字（Evidence 可来自外部 repo 的只读快照）

---

## 八、参考

- [llm_wiki.md](../llm_wiki.md) — Karpathy 原文：raw 为 source of truth
- [101-wiki-maintenance-model.md](101-wiki-maintenance-model.md) — 维护入口
- [109-agent-exploration.md](109-agent-exploration.md) — Agent 与代码理解
