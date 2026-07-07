# Wiki 维护模型：以 raw 为真相源还是以 wiki 为真相源？

> 本文档记录 Wiki 维护模型的核心设计决策——raw 源文件和 wiki 派生页面哪个是维护入口。
> 最后更新：2026-07-04

---

## 核心问题

`ruminate lint` 检测到 issue（矛盾、过时、死链等）之后，用户应该怎么修复？修复应该在 raw/ 源文件层做，还是在 wiki/ 派生页面层做？两种选择各有利弊，目前 ruminate 两边的工具链都不完善，需要明确维护模型才能确定后续开发方向。

## 背景：两个事实层

```text
raw/（少数源文件）  ──ingest──►  wiki/（大量派生页面）
    ↑                                ↑
  你掌控                           LLM 生成
```

- **raw/** 包含用户策展的原始资料（文章、笔记、论文等）。数量少、内容集中，是 Wiki 的来源。
- **wiki/** 包含 LLM 生成的派生页面（summaries、entities、concepts、synthesis）。数量多、跨文件散落，是 Wiki 的实际产出。
- ingest 将 raw 转化为 wiki：一个 source 可能生成/更新多个 wiki page；一个 wiki page 可能接收来自多个 source 的引用；一个 wiki page 也可能来自多个 source 的贡献；

## 决策：以 raw 为真相源，以 wiki 为最强大脑

**raw 是历史存档，是真相源，但是 wiki 页面是知识的主要载体和维护入口，是最强大脑的体现形式。**

核心理由：

1. **Ruminate 的目标是"最强大脑"，不是图书馆**。大脑记住的是理解、消化、综合后的知识，而不是强调记住一堆旧文档。就像你学会了勾股定理之后不会每次用都去翻《几何原本》，你实在要求证的时候，也可以重新搜索其他信息源——包括 web——而不必局限在 raw/ 里找。raw 虽然是真相，但是当我"求证""扩展"的时候可以借助 LLM 和 toolcall 来延伸的。
2. **同时维护 raw 和 wiki 两套系统不可持续**。raw 里的文件可能来自不同 repo、不同软件、不同时期，要求用户在修改 wiki 的同时也去更新那些原始来源，维护负担翻倍，最终两套都不维护。

## 由此确定的开发方向

1. **`ruminate lint --fix`**：LLM 直接从 lint issue 出发修改 wiki pages，ruminate lint 执行后要把 issues 序列化后输出到固定的隐藏文件，然后执行 lint --fix 时如果这个文件存在就处理这个文件中的 issue，如果不存在就先创建再读取再进行修复。如果这个文件是 1day 前创建的就重新执行输出这个 issues 文件。
2. **Contributing source 记录**（远期）：每个 wiki page 的 frontmatter 中记录哪些 raw source 贡献了内容，用于追溯真相源中那些内容应该被更新，也要提示我们更新。
3. **`ruminate rebuild`**（远期）：从 raw 重建 wiki 的便捷工具，但不要求 raw 被持续维护。

## 参考

- [Ingest 与 Lint 的职责分离](100-ingest-lint-separation.md)
