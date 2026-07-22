# Ruminate

> *"The wise man knows that it is better to sit on the bank of a remote mountain stream than to be emperor of the whole world."* — but between the two of us, the real wise move is to write it all down.

**Ruminate**（反刍）是一个 AI 驱动的个人知识库系统。它不只是存储你的笔记，而是帮你持续**消化**信息——收集、整理、关联、更新、淘汰——让你的知识像活的有机体一样不断生长和进化。

---

## 为什么需要 Ruminate？

### 好记性不如烂笔头

我们都听过这句话，但真正的问题是：**烂笔头写下来之后呢？**

大多数人陷入了两种困境：

1. **写而不理**：笔记越积越多，文件夹越来越深，但从来不去回顾。知识变成了数字尘埃。
2. **理而不及**：你确实在维护笔记——更新博客、整理错题本、修订电子书——但维护成本随着知识量增长而指数级上升。更新一个观点需要追溯几十处引用，发现一条矛盾需要翻遍所有相关文档。

Ruminate 要解决的问题是：**让每个人都能科学地维护自己头脑中的知识库。**

### 从 RAG 到 Wiki：知识需要积累

现有的 AI 知识工具（NotebookLM、ChatGPT 文件上传、各类 RAG 系统）有一个共同缺陷：**每次查询都是从零开始。** AI 临时检索、临时拼凑、临时推理——然后答案消失在聊天记录里。下次你问一个相关但更深的问题，它又重新来过。**没有任何积累。**

Ruminate 的核心思想不同：**AI 增量构建并持续维护一个持久化的知识 Wiki**——一个结构化的、相互链接的 Markdown 文件集合，它位于你和原始资料之间。

当你添加一份新资料时，AI 不只是索引它。它读它、提取关键信息、整合进已有 Wiki——更新实体页面、修订主题摘要、标注新旧矛盾、强化或质疑当前的综合结论。**知识被编译一次，然后持续保鲜。**

这就是关键区别：**Wiki 是一个持久化的、复合增长的产物。** 交叉引用已经就位，矛盾已经标注，综合结论已经反映你读过的所有内容。每添加一份资料、每提出一个问题，Wiki 都会变得更丰富。

---

## 快速开始

### 前置要求

- [Ollama](https://ollama.com/)（本地 LLM 推理和嵌入服务）
- Go 1.22+
- Git

### 安装

```bash
make build               # 编译 ruminate 二进制
make install             # 安装到 $GOPATH/bin
```

### 基本用法

```bash
# 1. 初始化知识库
ruminate init --wiki ./my-wiki

# 2. 摄入资料
ruminate ingest article.md -t article          # 本地 Markdown 文件
ruminate ingest paper.txt -t paper             # 纯文本论文
ruminate ingest https://example.com/post       # 网页 URL

# 3. 全文搜索
ruminate find "机器学习"                        # 关键词搜索，高亮片段

# 4. AI 问答
ruminate ask "如何理解反向传播？"                # 基于 Wiki 的流式问答
ruminate ask "什么是过拟合" --effort balanced   # 多角度查询扩展检索
ruminate ask "什么是过拟合" --effort thorough   # HyDE 假设文档检索
ruminate ask "什么是 KL 散度" --save            # 好答案回写 Wiki

# 5. 知识库巡检
ruminate lint                                  # 健康检查（矛盾、孤立、过时、断链）
ruminate lint --check broken_link,orphan       # 指定检查项
ruminate lint --staleness-days 30              # 自定义过期阈值

# 6. 增量同步（从源仓库）
ruminate sync --repo ~/notes -t note            # 同步笔记仓库的变更
ruminate sync --repo ~/notes --dry-run           # 预览变更而不实际执行

# 7. Git 钩子（自动同步）
ruminate hook install --repo ~/notes             # 安装 post-commit 钩子，每次提交自动触发 sync
ruminate hook uninstall --repo ~/notes           # 移除钩子

## 8. 重建索引
ruminate reindex
```

---

## 核心架构

### 三层设计

```
┌──────────────────────────────────────────────────┐
│                 原始资料层                         │
│  文章、论文、笔记、网页……                           │
│  只读 — 永远不被修改，是你的事实来源                 │
└──────────────────────────────────────────────────┘
                       ↓ AI 读取
┌──────────────────────────────────────────────────┐
│                 知识 Wiki 层                       │
│  摘要页、实体页、概念页、对比页、综合索引……           │
│  完全由 AI 生成和维护，你只管阅读和提问               │
└──────────────────────────────────────────────────┘
                       ↓ 受控于
┌──────────────────────────────────────────────────┐
│                 Schema 配置层                      │
│  定义 Wiki 结构、命名规范、工作流、输出格式……         │
│  你和 AI 共同演化，越用越好                          │
└──────────────────────────────────────────────────┘
```

### 检索管道

`ruminate ask` 的完整检索管道（可配置搜索力度）：

```
Query
  │
  ├── [fast] 直接向量检索
  ├── [balanced] 查询扩展：LLM 生成 2-3 变体 → 各自检索 → RRF 合并变体结果
  └── [thorough] HyDE：LLM 生成假设文档 → 嵌入 → 向量检索
  │
  ├── FTS5 全文检索（CJK bigram 分词） → RRF 混合融合
  ├── MMR 多样性重排（避免单一种类结果垄断）
  ├── LLM listwise 重排序（过滤无关结果 + 精确排序）
  └── 返回 top-N 结果 → LLM 综合回答 + 引用
```

---

## 当前可用功能

### 摄入（Ingest）

| 能力         | 说明                                                                                   |
| ------------ | -------------------------------------------------------------------------------------- |
| 文件摄入     | `.md` `.txt` `.json` `.yaml` `.csv` `.html` `.org` 等，可扩展 FileReader |
| URL 摄入     | 自动抓取网页内容，提取标题                                                             |
| 流式分析     | LLM 流式输出，实时看到 AI 的思考过程                                                   |
| Git 版本控制 | 每次摄入自动 Git commit                                                                |
| 内容校验     | 显式的文件大小和内容长度限制，拒绝时给出明确原因                                       |

### 同步（Sync）

| 能力         | 说明                                                       |
| ------------ | ---------------------------------------------------------- |
| 增量同步     | 检测源仓库变更（增/改/删/重命名），仅处理差异              |
| 首次全量同步 | 首次运行时摄入所有已跟踪文件                               |
| 文件类型过滤 | 跳过不支持的文件类型，无需手动筛选                         |
| 删除处理     | 源文件删除时保留 Wiki 内容 + 标记警告                      |
| Git 钩子     | `ruminate hook install` 自动在 `git commit` 时触发同步 |
| 干运行模式   | `--dry-run` 预览变更而不实际执行                         |

### 查询（Query）

| 能力       | 说明                                                                          |
| ---------- | ----------------------------------------------------------------------------- |
| 全文搜索   | SQLite FTS5 + CJK bigram 分词，BM25 排序，片段高亮                            |
| 向量检索   | Ollama embedding 语义搜索                                                     |
| 混合检索   | FTS5 + 向量 RRF 融合，互补召回                                                |
| 查询扩展   | `--effort balanced`：多查询变体 + RRF；`--effort thorough`：HyDE 假设文档 |
| MMR 多样性 | 去重去聚类，保证结果多样性                                                    |
| LLM 重排序 | listwise 相关度排序，自动过滤不相关候选项                                     |
| 流式回答   | `ruminate ask` 实时流式输出                                                 |
| 引用溯源   | 每个回答附带来源页面，可追溯验证                                              |
| 回答回写   | `--save` 将优质回答保存为 Wiki 页面                                         |

### 巡检（Lint）

| 检查项       | 说明                                              |
| ------------ | ------------------------------------------------- |
| 断链检测     | 检查所有`[[WikiLink]]` 目标是否存在             |
| 孤立页面     | 发现没有入链或完全无链接的页面                    |
| 过时内容     | 按可配置天数阈值检测长期未更新的页面              |
| 矛盾检测     | 共享主题的页面 → LLM 深度分析事实矛盾            |
| 一词多义识别 | 自动区分同名不同义（如"元宝"=货币 vs 猫），不误报 |
| 抑制机制     | `.ruminate/suppressions.yaml` 手动抑制误报      |
| 报告输出     | 纯文本或 JSON 格式，按严重度排序                  |

### 可观测性

| 能力     | 说明                                      |
| -------- | ----------------------------------------- |
| 管道追踪 | `-v/--verbose` 启用 span-based 追踪日志 |
| 分级输出 | 实时显示每阶段的候选数、延迟、文档 ID     |

---

## 项目状态

 **核心管道已完整可用。** CLI 可通过 `ingest` → `find`/`ask` → `lint` 完成完整的知识摄入、检索和巡检闭环。`sync` 提供与源仓库的增量同步能力。

| Phase   | 名称               | 状态                                          |
| ------- | ------------------ | --------------------------------------------- |
| Phase 0 | 项目骨架与基础设施 | ✅ 已完成                                     |
| Phase 1 | Wiki 核心存储      | ✅ 已完成                                     |
| Phase 2 | LLM 集成与摄入     | ✅ 已完成                                     |
| Phase 3 | 查询与检索         | ✅ 已完成                                     |
| Phase 4 | 巡检与 Web 服务    | 🔄 进行中（lint + sync 已完成，serve 待开发） |
| Phase 5 | Web 前端           | ⬜ 未开始                                     |
| Phase 6 | 增强与打磨         | ⬜ 未开始                                     |

### 下一步计划

- **HTTP Server + REST API**：`ruminate serve` 启动 API 服务
- **Web 前端**：Wiki 浏览、AI 对话、摄入管理、知识图谱
- **多 Provider 支持**：DeepSeek、OpenAI 兼容接口

---

## 与卡帕西 (Andrej Karpathy) 的共鸣

我经常写总结、博客、电子书，使用的编辑器、文件格式也不完全统一，如何维护这些碎片化的知识，是我经常思考的问题：

- 在 LLM 大爆发之前：我总是想找类似 Notion/Docmost/Appflowy/Affine 之类的功能强大并且方便知识管理的编辑器；
- 在 LLM 大爆发之后：我意识到应该基于 AI 构建一个更强大的知识库入口，它可以从不同的信息源收集并持续打磨、咀嚼、更新这些知识。

本项目受到 [Karpathy 的 LLM Wiki](llm_wiki.md) 理念的直接启发，他在其中提出了完全相同的核心洞察：

> *"The tedious part of maintaining a knowledge base is not the reading or the thinking — it's the bookkeeping. LLMs don't get bored, don't forget to update a cross-reference, and can touch 15 files in one pass."*

维护知识库最痛苦的不是阅读或思考，而是**记账**——更新交叉引用、保持摘要时效、标注新旧矛盾、在几十个页面之间保持一致性。人类放弃 Wiki 是因为维护负担的增长速度超过了价值增长速度。LLM 不会厌倦、不会忘记更新引用、一次操作可以触及 15 个文件。

Ruminate 将这个理念从一个 "idea file" 变成了一个**可交付的软件系统**。

---

## 技术栈

| 层           | 技术                         | 原因                              |
| ------------ | ---------------------------- | --------------------------------- |
| 后端         | Go                           | 开发者熟悉；强大的 CLI 和并发支持 |
| 前端         | TypeScript + React (Vite)    | AI 友好；可读可参与               |
| 存储         | Markdown + Git + SQLite FTS5 | Git 原生版本控制；嵌入式全文搜索  |
| LLM Provider | Ollama（推理 + 嵌入）        | 本地优先；统一接入                |
| 向量存储     | 自研本地向量索引             | 零依赖，与 Wiki 存储同目录        |
| CLI 框架     | Cobra                        | Go 生态标准 CLI 库                |

## 架构原则

1. **本地优先**：所有数据在用户磁盘上，不依赖任何云服务
2. **Git 原生**：Wiki 就是 Markdown 文件的 Git 仓库，自带版本历史和回滚能力
3. **Markdown 即真相**：Wiki 页面是纯 Markdown，任何编辑器（Obsidian、VS Code 等）都能阅读
4. **Provider 抽象**：LLM 和 Embedding 接口可切换，核心逻辑不绑定特定厂商
5. **CLI 优先，再 Web**：核心操作通过 CLI 完成，Web UI 通过 HTTP API 层叠其上
6. **增量采纳**：从简单开始，按需增加功能

## 开发

```bash
make build       # 编译 CLI 二进制
make test        # 运行所有测试
make lint        # 运行 linters
make dev         # 启动开发服务器（后端 + 前端）
make install     # 安装 CLI 到 $GOPATH/bin
```

## 许可

MIT License
