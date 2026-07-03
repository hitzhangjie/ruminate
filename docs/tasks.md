# 任务进度追踪

> 最后更新：2026-07-03

---

## 总览

| Phase | 名称 | 状态 | 进度 |
|-------|------|------|------|
| Phase 0 | 项目骨架与基础设施 | ✅ 已完成 | 5/5 |
| Phase 1 | Wiki 核心存储 | ✅ 已完成 | 4/4 |
| Phase 2 | LLM 集成与摄入 | ✅ 已完成 | 4/4 |
| Phase 3 | 查询与检索 | ✅ 已完成 | 3/3 |
| Phase 4 | 巡检与 Web 服务 | ⬜ 未开始 | 0/3 |
| Phase 5 | Web 前端 | ⬜ 未开始 | 0/4 |
| Phase 6 | 增强与打磨 (P1/P2) | ⬜ 未开始 | 0/4 |

---

## Phase 0: 项目骨架与基础设施

**目标**：搭建可构建、可测试、可运行的项目骨架。

| # | 任务 | 状态 | 负责人 | 备注 |
|---|------|------|--------|------|
| 0.1 | Go 项目初始化：go mod, 目录结构, Makefile | ✅ | — | 见 [architecture.md](architecture.md) 目录结构 |
| 0.2 | CLI 框架：cobra 子命令 (ingest/ask/find/lint/serve/config) | ✅ | — | 命令注册 + 占位实现 |
| 0.3 | 配置系统：YAML 配置文件，LLM provider/Wiki 路径等 | ✅ | — | 默认配置 + 配置文件查找 |
| 0.4 | Git 工具封装：init/add/commit/log | ✅ | — | 基于 os/exec |
| 0.5 | 前端项目初始化：Vite + React + TypeScript 脚手架 | ✅ | — | 基础路由 + API 客户端骨架 |

**可交付**：
- `make build` 产出 `ruminate` 二进制
- `ruminate config` 可查看默认配置
- `make dev` 启动前端开发服务器（空白页面）

---

## Phase 1: Wiki 核心存储

**目标**：Wiki 目录初始化、页面 CRUD、索引和日志系统的核心能力。

| # | 任务 | 状态 | 负责人 | 备注 |
|---|------|------|--------|------|
| 1.1 | Wiki 目录结构定义与初始化 | ✅ | — | raw/ wiki/ index.md log.md schema.md |
| 1.2 | Index 管理器：index.md 读写更新 + SQLite FTS5 | ✅ | — | 使用 modernc.org/sqlite (纯 Go，含 FTS5) |
| 1.3 | Wiki 页面 CRUD：创建/读取/更新/删除 Markdown 页面 | ✅ | — | 支持 WikiLink [[page]] 解析 |
| 1.4 | 日志系统：log.md 结构化追加写入 | ✅ | — | 统一格式：## [日期] 操作类型 | 标题 |

**可交付**：
- `ruminate init` 初始化 Wiki 目录
- 程序化创建/读取 Wiki 页面
- 页面变更自动更新 FTS5 索引
- log.md 记录所有写操作

---

## Phase 2: LLM 集成与摄入

**目标**：连接 Ollama，实现完整的资料摄入流程。

| # | 任务 | 状态 | 负责人 | 备注 |
|---|------|------|--------|------|
| 2.1 | LLM Provider 抽象：推理接口定义 + Ollama 实现 | ✅ | — | Chat/ChatStream |
| 2.2 | Embedding Provider 抽象：嵌入接口定义 + Ollama 实现 | ✅ | — | 暂用于 P1 语义搜索，先定义接口 |
| 2.3 | Ingest 引擎：读取源 → LLM 分析 → 创建/更新页面 → 更新索引 → Git commit | ✅ | — | 核心业务流程 |
| 2.4 | CLI 集成：`ruminate ingest <file/url>` 端到端可工作 | ✅ | — | 支持 .md/.txt/URL |

**可交付**：
- `ruminate ingest article.md` 完成一次完整的知识摄入
- 摄入结果反映在 Wiki 页面、index.md、log.md 中
- Git 自动提交摄入变更

---

## Phase 3: 查询与检索

**目标**：实现全文检索和 AI 问答两种查询方式。

| # | 任务 | 状态 | 负责人 | 备注 |
|---|------|------|--------|------|
| 3.1 | 全文检索：SQLite FTS5 搜索，`ruminate find` 命令 | ✅ | — | 支持 BM25 排序、片段高亮 |
| 3.2 | AI 问答：`ruminate ask` 检索相关页 → LLM 综合 → 引用回答 | ✅ | — | 流式输出支持 |
| 3.3 | 回答回写：将好的问答结果保存为 Wiki 页面 | ✅ | — | `ruminate ask --save "..."` |

**可交付**：
- `ruminate find "关键词"` 返回匹配页面列表（高亮片段）
- `ruminate ask "问题"` 返回带引用的 AI 综合回答
- 问答结果可选回写 Wiki

---

## Phase 4: 巡检与 Web 服务

**目标**：实现知识巡检功能和 HTTP API 服务层。

| # | 任务 | 状态 | 负责人 | 备注 |
|---|------|------|--------|------|
| 4.1 | Lint 引擎：矛盾检测、孤立页面、过时内容、缺失链接 | ⬜ | — | `ruminate lint` 命令 |
| 4.2 | HTTP Server：REST API + WebSocket | ⬜ | — | 使用 [chi](https://github.com/go-chi/chi) 路由 |
| 4.3 | `ruminate serve`：一键启动后端 + 代理前端 | ⬜ | — | 开发模式代理 Vite，生产模式嵌入静态文件 |

**可交付**：
- `ruminate lint` 输出健康检查报告
- `ruminate serve` 启动 API 服务
- API 可通过 curl 测试

---

## Phase 5: Web 前端

**目标**：提供浏览器端的 Wiki 浏览、AI 对话和可视化管理界面。

| # | 任务 | 状态 | 负责人 | 备注 |
|---|------|------|--------|------|
| 5.1 | Wiki 浏览视图：页面列表、页面内容、链接跳转 | ⬜ | — | 左侧目录树 + 右侧内容 |
| 5.2 | AI 对话视图：聊天界面，流式问答交互 | ⬜ | — | WebSocket 流式输出 |
| 5.3 | 摄入管理：上传/粘贴资料、查看摄入历史 | ⬜ | — | 拖拽上传 + 进度显示 |
| 5.4 | 知识图谱：D3/vis.js 页面关系图 | ⬜ | — | 节点=页面，边=链接 |

**可交付**：
- `ruminate serve` 后在浏览器中使用完整功能
- Wiki 浏览 + AI 对话 + 摄入管理 + 图谱可视化

---

## Phase 6: 增强与打磨 (P1/P2)

**目标**：P1 能力补充、性能优化、体验打磨。

| # | 任务 | 状态 | 负责人 | 备注 |
|---|------|------|--------|------|
| 6.1 | 增量重建：`ruminate rebuild` 从头重建 Wiki | ⬜ | — | 从 raw/ 重新处理所有源 |
| 6.2 | 向量检索：embedding + 本地向量存储，语义搜索 | ⬜ | — | 评估 LanceDB / Chroma |
| 6.3 | 多 Provider 支持：DeepSeek、OpenAI 兼容接口 | ⬜ | — | 推理和嵌入可独立配置 |
| 6.4 | 高级输出格式：Marp 幻灯片、图表导出 | ⬜ | — | |

**可交付**：
- 多 AI Provider 可切换
- 语义搜索可用
- Wiki 可完全重建

---

## 状态标记说明

| 标记 | 含义 |
|------|------|
| ⬜ | 未开始 |
| 🔄 | 进行中 |
| ✅ | 已完成 |
| ⏸️ | 暂停 |
| ❌ | 取消 |

---

## 变更记录

| 日期 | 变更 |
|------|------|
| 2026-06-30 | 初始任务拆分，6 个 Phase 共 27 个子任务 |
| 2026-06-30 | Phase 0 完成：项目骨架、CLI、配置、Git 封装、前端脚手架 |
| 2026-07-01 | Phase 1 完成：Wiki 核心存储 (CRUD, Index, WikiLink, Log, Init) |
| 2026-07-02 | Phase 2 完成：LLM 集成与摄入 (Provider, Embedder, Ingest Engine, CLI) |
| 2026-07-03 | Phase 3 完成：查询与检索 (FTS5 Search with snippets, AI Q&A with streaming, Answer writeback) |
