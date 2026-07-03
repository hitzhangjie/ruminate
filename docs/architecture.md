# 技术方案

> 本文档描述 Ruminate 的技术架构、模块设计、接口约定和关键技术决策。
> 最后更新：2026-06-30

---

## 一、系统架构总览

```
┌─────────────────────────────────────────────────────┐
│                   ruminate CLI                       │
│   ingest │ ask │ find │ lint │ serve │ config        │
│                  (cobra commands)                     │
└──────────┬──────────────────────┬────────────────────┘
           │                      │
     Go Backend             HTTP/WS API (chi router)
           │                      │
  ┌────────┴──────────┐    ┌──────┴──────────────┐
  │  Ingest Engine    │    │  Web Frontend        │
  │  Query Engine     │    │  Vite + React + TS   │
  │  Lint Engine      │    │  - Wiki 浏览          │
  │  Wiki Manager     │    │  - AI 对话           │
  │  Search Manager   │    │  - 资料摄入管理       │
  └────────┬──────────┘    │  - 知识图谱          │
           │               └─────────────────────┘
  ┌────────┴──────────┐
  │  LLM Provider 层   │
  │  ├─ Ollama (推理)  │
  │  └─ Ollama (嵌入)  │
  │  (未来扩展:        │
  │   DeepSeek, OAIC)  │
  └────────┬──────────┘
           │
  ┌────────┴──────────┐
  │  存储层            │
  │  ├─ Markdown+Git   │── Wiki 文件 (raw/ + wiki/)
  │  ├─ SQLite FTS5    │── 全文检索索引
  │  └─ Schema 配置    │── schema.md (Wiki 结构定义)
  └───────────────────┘
```

## 二、目录结构

```
ruminate/
├── cmd/
│   └── ruminate/
│       └── main.go              # CLI 入口
├── internal/
│   ├── cmd/                     # Cobra 子命令实现
│   │   ├── root.go
│   │   ├── ingest.go
│   │   ├── ask.go
│   │   ├── find.go
│   │   ├── lint.go
│   │   ├── serve.go
│   │   └── config.go
│   ├── ingest/                  # 摄入引擎
│   │   ├── engine.go            # 摄入主流程
│   │   ├── reader.go            # 源文件读取（md/txt/url/pdf）
│   │   └── processor.go         # LLM 分析与页面生成
│   ├── query/                   # 查询引擎
│   │   ├── engine.go            # 查询主流程
│   │   ├── finder.go            # 全文检索（FTS5）
│   │   └── asker.go             # AI 问答（检索→综合→引用）
│   ├── lint/                    # 巡检引擎
│   │   └── engine.go            # 健康检查主流程
│   ├── wiki/                    # Wiki 管理
│   │   ├── manager.go           # 页面 CRUD
│   │   ├── index.go             # index.md + FTS5 索引管理
│   │   ├── log.go               # log.md 日志系统
│   │   └── schema.go            # schema.md 读取和验证
│   ├── llm/                     # LLM Provider 抽象
│   │   ├── provider.go          # 推理接口定义
│   │   ├── embedder.go          # 嵌入接口定义
│   │   ├── ollama.go            # Ollama 实现
│   │   └── openai.go            # OpenAI 兼容实现 (未来)
│   ├── search/                  # 搜索引擎
│   │   ├── fts.go               # SQLite FTS5 封装
│   │   └── vector.go            # 向量检索 (P1)
│   ├── gitwrap/                 # Git 操作封装
│   │   └── git.go
│   ├── config/                  # 配置管理
│   │   └── config.go
│   └── serve/                   # HTTP API 服务
│       ├── server.go
│       ├── router.go
│       ├── handler/
│       │   ├── ingest.go
│       │   ├── query.go
│       │   ├── wiki.go
│       │   └── search.go
│       └── middleware/
├── web/                         # 前端 (Vite + React + TS)
│   ├── src/
│   │   ├── App.tsx
│   │   ├── pages/
│   │   │   ├── WikiBrowse.tsx
│   │   │   ├── AIChat.tsx
│   │   │   ├── IngestManage.tsx
│   │   │   └── GraphView.tsx
│   │   ├── components/
│   │   ├── hooks/
│   │   └── api/                 # 后端 API 客户端
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── docs/                        # 项目文档
│   ├── requirements.md
│   ├── architecture.md
│   └── tasks.md
├── testdata/                    # 测试数据
│   ├── sources/                 # 模拟原始资料
│   └── wiki/                    # 模拟 Wiki 输出
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 三、核心模块设计

### 3.1 Wiki 存储模型

```
<wiki_root>/                    # 默认为当前目录的 ruminate_wiki/
├── raw/                        # 原始资料（按用户分类标签组织：article, paper, note, blog...）
│   └── <source-type>/           # 按需创建，source-type 由用户自定义
├── wiki/                       # AI 生成的 Wiki 页面
│   ├── summaries/              # 摘要页
│   ├── entities/               # 实体页（人物、事件、术语...）
│   ├── concepts/               # 概念页
│   └── synthesis/              # 综合/对比/综述页
├── index.md                    # 内容索引（人类可读目录）
├── log.md                      # 操作日志（时间线）
├── schema.md                   # Wiki 结构定义和写作规范
└── .ruminate/                  # 内部状态（不入版本控制）
    └── fts.db                  # SQLite FTS5 索引
```

### 3.2 LLM Provider 接口

```go
// 推理接口
type LLMProvider interface {
    // Chat 发送对话请求，返回完整回复
    Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error)
    // ChatStream 流式对话
    ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan Chunk, error)
}

// 嵌入接口
type EmbeddingProvider interface {
    // Embed 将文本转为向量
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    // EmbedQuery 将查询文本转为向量（某些模型对查询和文档使用不同编码）
    EmbedQuery(ctx context.Context, text string) ([]float32, error)
}
```

### 3.3 摄入流程

```
用户执行: ruminate ingest <source_path>

1. Reader 读取源文件 → 文本内容 + 元数据
2. LLM 分析 → 提取实体、概念、摘要、关键观点
3. Wiki Updater:
   a. 创建/更新 summaries/<source>.md
   b. 创建/更新 entities/*.md（受影响实体）
   c. 创建/更新 concepts/*.md（受影响概念）
   d. 更新交叉引用（双向链接）
4. Index Updater:
   a. 更新 index.md 目录
   b. 更新 SQLite FTS5 索引
5. Log Writer → log.md 追加摄入记录
6. Git Committer → git add + git commit
```

### 3.4 查询流程

**直接检索 (find):**
```
用户执行: ruminate find "关键词"

1. SQLite FTS5 搜索 → 匹配页面列表（带 BM25 评分）
2. 格式化输出 → 页面标题、匹配片段、链接
```

**AI 问答 (ask):**
```
用户执行: ruminate ask "问题"

1. 检索相关页面 → FTS5 搜索（pages_fts），按 BM25 评分排序取 top-N
2. 读取候选页面内容（按相关性截取 top-N）
3. 构建 LLM prompt → 系统提示 + 相关页面内容 + 用户问题
4. LLM 综合回答 → 带引用标注
5. 可选：将问答结果回写 Wiki
```

### 3.5 索引策略

**核心原则**：`pages_fts` (SQLite FTS5) 是检索的唯一来源。`find` 和 `ask` 都直接查询 FTS5，不依赖 `index.md`。

**SQLite FTS5 索引**（机器检索，搜索的唯一真相源）:
- 为所有 Wiki 页面和 Raw 源文件建立 FTS5 全文索引
- 支持 BM25 相关性排序
- 支持前缀匹配、短语查询
- 索引在每次 Wiki 变更后增量更新
- Raw 源文件仅存在于 FTS5 索引中，不出现在 index.md

**index.md**（人类可读，从 FTS5 派生）:
- 纯 Markdown 文件，方便用任意编辑器浏览
- 由 `pages_fts` 重建（`rebuildIndexMd`），而非反过来
- 只包含 Wiki 页面，不包含 Raw 源文件
```markdown
# Wiki Index

## Summaries
- [Karpathy: LLM Wiki](wiki/summaries/llm-wiki-idea.md) — 2026-04-02 · 核心思路与架构设计

## Entities
- [Andrej Karpathy](wiki/entities/karpathy.md) — AI 研究者，LLM Wiki 理念提出者
- ...

## Concepts
- [RAG vs Wiki](wiki/concepts/rag-vs-wiki.md) — 两种知识管理范式的对比
- ...

## Synthesis
- [知识库设计哲学](wiki/synthesis/design-philosophy.md) — 综合各方观点的综述
- ...
```

### 3.6 Git 操作约定

- Wiki 根目录初始化为 Git 仓库（如果没有的话，`ruminate init`）
- 每次摄入/查询回写/巡检修复自动提交
- Commit 格式：`[ingest] 源文件标题` / `[query] 问题摘要` / `[lint] 修复 N 处`
- 支持 `ruminate log` 查看操作历史（Git log 包装）
- 回滚：标准 `git revert`（用户自行操作或 `ruminate undo`）

## 四、API 设计（Web 后端）

### 4.1 REST API

```
GET    /api/health              # 健康检查
GET    /api/wiki/pages           # 页面列表
GET    /api/wiki/pages/:id       # 页面内容
GET    /api/wiki/pages/:id/links # 页面入链/出链
GET    /api/search?q=            # 全文搜索
POST   /api/ask                  # AI 问答
POST   /api/ingest               # 摄入（上传文件或粘贴文本）
POST   /api/lint                 # 触发巡检
GET    /api/lint/status          # 巡检结果
GET    /api/config               # 获取配置
PUT    /api/config               # 更新配置
```

### 4.2 WebSocket

```
WS /ws/chat      # 流式 AI 对话
```

## 五、前端路由

```
/                # 首页/Dashboard（最近更新、统计）
/wiki            # Wiki 浏览（页面树、搜索）
/wiki/:page      # 单个 Wiki 页面
/chat            # AI 对话界面
/ingest          # 资料摄入管理
/graph           # 知识图谱
/settings        # 配置页面
```

## 六、Schema 设计

schema.md 是 Wiki 的"宪法"，定义：

```yaml
# 页面类型
page_types:
  - summary      # 资料摘要
  - entity       # 实体（人物、事件、术语等）
  - concept      # 概念/主题
  - synthesis    # 综合/对比/综述

# 命名规范
naming:
  summary: "{source_title}.md"
  entity: "{entity_name}.md"

# 交叉引用格式
linking:
  style: "[[page-name]]"       # WikiLink 风格
  bidirectional: true          # 自动维护反向链接

# 摄入规则
ingest:
  max_summary_length: 500      # 摘要最长字数
  extract_entities: true       # 是否提取实体
  extract_concepts: true       # 是否提取概念
  update_existing: true        # 是否更新已有页面

# 巡检规则
lint:
  check_contradictions: true   # 检测矛盾
  check_orphans: true          # 检测孤立页面
  check_staleness_days: 90     # 超过此天数标记为可能过时
  suggest_new_pages: true      # 建议新建页面
```

## 七、关键技术决策

1. **不用向量数据库（P0）**：SQLite FTS5 已足够。P1 引入 embedding + 本地向量存储。
2. **不用消息队列**：单用户本地工具，同步操作即可。Web 端的长任务用简单的 goroutine + 状态轮询。
3. **不做用户系统**：本地工具，单用户。团队协作是 P2。
4. **不做插件系统**（P0）：MCP 集成在 P2 实现。
5. **Git 作为唯一真相源**：不搞数据库存储 Wiki 内容。Markdown 文件 + Git = 可移植、可合并、永不锁定。
