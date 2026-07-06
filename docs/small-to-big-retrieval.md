# Small-to-Big 检索

## 概述

Small-to-big retrieval 是 RAG（检索增强生成）中的一种检索策略，通过**小粒度索引、大上下文返回**的方式，同时兼顾检索精度和生成质量。

## 动机

| 问题 | 解决方式 |
|------|----------|
| 大 chunk 做 embedding 时语义被稀释，容易召回到不相关的内容 | 用小粒度的 chunk 做 embedding 和检索 |
| 小 chunk 缺乏上下文，LLM 只看到碎片无法回答 | 召回后将父级"大块"拼接返回 |

## 核心流程

1. **Small（索引/检索）**：将文档切成较小的 chunk（如句子级别），用这些小 chunk 做向量检索。小块语义更聚焦，检索精度更高。

2. **Big（返回）**：检索命中后，将命中 chunk 所属的**更大上下文**（如段落或整个文档）一并返回给 LLM，保证有足够上下文理解答案。

```
文档 ──切分──▶ 大块 (parent)
  │              │
  │         再切分为小块 (children)
  │              │
  │         embedding + 索引
  │              │
  └── 检索命中 child ──▶ 返回对应 parent
```

## 典型实现方式

### Sentence Window Retrieval

- embedding 以句子为单位
- 检索命中后返回该句子前后各 N 句作为上下文窗口
- 实现简单，窗口大小可调

### Hierarchical Chunking（父子分块）

- 维护父子 chunk 关系（如 `chunk_id → parent_id`）
- 检索用子节点（小粒度）
- 返回用父节点（大粒度）
- 父节点可以是一段、一节，甚至整篇文档

### 多级层次

```
文档 → 节 → 段落 → 句子
       ↑      ↑       ↑
     大块   中块    小块（索引/检索粒度）
```

可以根据需要合并相邻的多个小块返回，或直接返回父节点。

## 优势

1. **检索精度高**：小块语义聚焦，embedding 更准确
2. **上下文完整**：返回大块保证 LLM 有足够信息理解问题
3. **灵活性**：窗口大小和父子关系可根据场景调整

## 与其他策略的关系

Small-to-big 可以与以下策略组合使用：

- **MMR 多样性**：在小粒度上做多样性过滤，再扩展到大块
- **HyDE**：用假设答案做小粒度检索，提升召回率
- **Query Expansion**：多路召回在小粒度上并行检索，合并后去重扩展父块
- **Rerank**：在小粒度上做粗筛+精排，命中后再扩展到父块

## 与 Ruminate 的差异

标准的 Small-to-Big 解决的是**文本切片粒度**问题——原文怎么切、检索用哪层、返回用哪层。Ruminate 走的是另一条路：

### Ruminate 的做法：语义蒸馏而非文本切分

```
Raw 原文（raw/ 目录）
  │
  └── LLM 理解、拆分 ──▶ concept, entity, summary 等语义组件
                              │
                              ├── 作为 pages 做 embedding + 索引
                              ├── 维护组件之间的链接关系
                              └── 可回溯到 raw 原文
```

**关键区别：**

| 维度 | 标准 Small-to-Big | Ruminate |
| ---- | ----------------- | -------- |
| 检索单元来源 | 原文机械切分（按句子/段落/token 数） | LLM 语义提取（concept、entity、summary） |
| 单元粒度 | 由切分策略决定（句子级到段落级） | 由语义边界自然决定，介于 chunk 和文章之间 |
| 上下文扩展方式 | 沿切分树向上找 parent chunk | 沿语义链接图遍历关联组件 |
| 信息密度 | 原文片段，可能有噪声 | 蒸馏后的知识组件，信息密度高 |

### 为什么 Small-to-Big 的思路不完全适用

Ruminate 的 pages 本身已经处于一个"中间态"——比 sentence chunk 大、比原文小，而且每个 page 是语义完整的（一个 concept 的完整描述、一个 entity 的所有属性），不像机械切分那样可能截断语义。

这意味着：

- **不需要再"big"**：召回一个 concept page，信息本身已经足够完整，不需要向上扩展到更大的父块。
- **上下文靠链接而非扩展**：如果确实需要更多上下文，应该沿 page 之间的链接关系扩展（concept → 关联 entity → summary → raw），而不是简单地返回更大的文本块。
- **去重和合并逻辑不同**：标准 Small-to-Big 是去重 parent chunk，Ruminate 需要的是沿知识图谱去重地展开关联节点。

### 可能的结合点

尽管路线不同，Small-to-Big 的部分思想仍可借鉴：

1. **检索后回填原文**：召回 concept/entity page 后，可以将关联的 raw 原文段落作为附件返回给 LLM，类似于"small=page, big=raw section"。
2. **多粒度混合检索**：同时在不同粒度建索引——concept（细）、summary（粗），查询时根据问题类型选择粒度或加权合并。

## 实现要点

1. 需要维护 chunk 到 parent 的映射关系（可通过 metadata 中的 `parent_id` 或 `doc_id` 实现）
2. 多个 child 命中同一个 parent 时需要去重
3. 可以在合并 parent 时按原有顺序拼接，保持文档结构
4. 检索时可以用较小的 top-K（因为最终返回的是合并去重后的 parent，实际上下文量会更大）

## 参考

- LlamaIndex 的 [SentenceWindowNodeParser](https://docs.llamaindex.ai/en/stable/examples/node_parsers/sentence_window_node_parser/) 和 [HierarchicalNodeParser](https://docs.llamaindex.ai/en/stable/module_guides/loading/node_parsers/modules/#hierarchicalnodeparser)
- LangChain 的 [ParentDocumentRetriever](https://python.langchain.com/docs/modules/data_connection/retrievers/parent_document_retriever/)
