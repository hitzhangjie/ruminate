# Wiki 维护模型：双真相与维护入口

> 本文档记录 Wiki 维护模型的核心设计决策——Evidence（raw/源码）与 Synthesis（wiki）的关系，以及修复入口。
> 最后更新：2026-08-04
>
> 延伸阅读：[108-dual-truth-and-layered-retrieval.md](108-dual-truth-and-layered-retrieval.md)

---

## 核心问题

`ruminate lint` 检测到 issue（矛盾、过时、死链等）之后，用户应该怎么修复？修复应该在 raw/ 源文件层做，还是在 wiki/ 派生页面层做？两种选择各有利弊，需要明确维护模型才能确定后续开发方向。

更根本的问题：当 **蒸馏必然有损** 时，系统把哪一层当作「可以永久依赖的真相」？

## 背景：两层事实

```text
Evidence（raw/、外部源仓、代码树…）  ──ingest──►  Synthesis（wiki/ 派生页面）
    ↑                                                      ↑
  事实真相                                               理解真相
  不可变快照 / 外部权威                                    LLM 生成与维护
```

- **Evidence**：用户策展或同步来的原始资料（文章、笔记、论文、代码指针等）。数量相对少、内容完整，是可核对的证据。
- **Synthesis（wiki/）**：LLM 生成的派生页面（summaries、entities、concepts、synthesis）。数量多、跨文件链接，是高密度的理解与综合。
- ingest 将 Evidence 编译为 Synthesis：一个 source 可能更新多个 wiki page；一个 wiki page 可能累积多个 source 的贡献。

## 决策：双真相（修订）

### 旧表述（2026-07-04，已修订）

> raw 是历史存档……wiki 是知识的主要载体和维护入口……求证时可以借助 LLM 和 web，不必局限在 raw/。

该表述对**个人学习/研究**仍然部分成立，但低估了：

1. 蒸馏丢失的细节可能仍关键，且 web 不可复现、不可审计  
2. 企业场景下「看起来自洽的 wiki」若与原文/代码分叉，危害更大  
3. 当前实现中 raw 虽归档，但 **未进入检索主路径**（且曾从 FTS 移除），等于查询协议上丢弃了 Evidence

### 新表述（2026-08-04）

**双真相原则：**

| 层 | 名称 | 回答的问题 | 查询中的角色 |
|----|------|------------|--------------|
| Evidence | 事实真相 | 原文/源码实际是什么？ | L2/L3 回退与引用 |
| Synthesis | 理解真相 | 综合之后我们如何理解？ | L1 默认入口 |

- **Wiki 仍是日常阅读与综合的主界面**（「最强大脑」的体现）——这点保留。
- **Evidence 不是可有可无的归档**：必须可打开、可检索（独立于 wiki 排序）、可被 agent 在不足时回退。详见 [108](108-dual-truth-and-layered-retrieval.md)。
- **求证顺序**：优先本库 Evidence（raw / 贡献源 / 代码锚点）；外部 web 仅作扩展，不得在有本地证据时默默覆盖。

### 维护入口（按问题类型）

| 问题类型 | 维护入口 | 说明 |
|----------|----------|------|
| 交叉引用、结构、消歧、综合表述 | **Wiki** | `lint --fix`、人工改 synthesis |
| 事实错误（日期、金额、API 行为、配置默认值） | **优先 Evidence 源** | 改源后 re-ingest / 定点更新；或改 wiki 但保留 sources 并标 confidence |
| 源不可控或已删除 | Wiki + 标注 | 保留最后 raw 快照；标记 stale / 低置信 |
| 实现与文档不一致 | 代码为行为真相 + wiki 记偏差 | 见 [109](109-agent-exploration.md) code anchors |

**不要求**用户对每一处 wiki 措辞双写回 raw（维护负担不可持续）。但 **系统不得在查询路径上假装 raw 不存在**。

### 仍成立的旧理由（收窄后）

1. Ruminate 目标仍是「编译后的最强大脑」，不是纯图书馆——**默认**读 Synthesis 是对的。  
2. 同时手工维护两套全文不可持续——所以 Evidence 以 **快照 + 外链源仓 + 贡献源指针** 存在，而不是强迫人改两遍。

## 由此确定的开发方向

1. **`ruminate lint --fix`**：LLM 从 lint issue 修改 **wiki** pages（理解层）。issues 序列化到隐藏文件；过期（如 1 day）则重跑检测。  
2. **Contributing sources（提升优先级）**：每个 wiki page frontmatter 记录贡献的 raw paths，支撑 L2 回退与事实追溯。见 [108](108-dual-truth-and-layered-retrieval.md) §4.1。  
3. **`ruminate rebuild`**：从 raw 重建 wiki，对抗蒸馏偏差沉积。  
4. **分层查询**：`ask --evidence wiki|auto|raw`；完整多步探索集成通用 agent（[109](109-agent-exploration.md)）。  
5. **事实修复规范**：lint 报告事实类 issue 时，尽量展示 contributing sources，提示「应核对的 Evidence」。

## 参考

- [Ingest 与 Lint 的职责分离](100-ingest-lint-separation.md)
- [双真相与分层召回](108-dual-truth-and-layered-retrieval.md)
- [Agent 探索与代码理解](109-agent-exploration.md)
- [Karpathy LLM Wiki](../llm_wiki.md) — Raw sources are source of truth
