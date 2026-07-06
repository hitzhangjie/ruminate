# Git Hook 集成：ruminate sync 设计方案

## 背景

Ruminate 已经达到基本可用状态。用户希望在常用的笔记仓库（GitHub repo）中配置 hook，当提交新文件时自动触发 ruminate 更新本地知识库，实现便捷同步。

## 核心场景

笔记仓库和 ruminate wiki 是**两个独立的 git 仓库**：

```
~/notes/          ← 笔记仓库（源）
  ├── note1.md
  ├── note2.md
  └── .git/

~/ruminate_wiki/  ← ruminate 知识库
  ├── wiki/
  ├── raw/
  ├── .ruminate/
  └── .git/
```

工作流：笔记仓库 commit → 触发 ruminate → 分析 diff → 更新知识库

## 命令设计

新增两个子命令：

| 命令 | 职责 |
|------|------|
| `ruminate sync` | 核心逻辑：diff 变更文件，增量同步到知识库 |
| `ruminate hook install` | 便捷安装：在源仓库中安装 git hook，自动调用 `ruminate sync` |

设计原则：
- **`sync` 是独立可用的**：可以手动执行，不依赖 hook。语义比 `hook commit` 更自然。
- **`hook install` 只做安装**：在目标仓库写入 post-commit hook 脚本，脚本内容就是调用 `ruminate sync`。职责分离清晰。
- **符合 Unix 哲学**：可组合的命令，各司其职。

---

### `ruminate sync`

```
ruminate sync [--repo <path>] [--source-type <type>] [--dry-run]
```

#### 核心流程

```
1. 确定源仓库路径（--repo 或当前目录）
2. 读取上次同步状态（存储在 wiki 的 .ruminate/sync_state.json）
3. git diff --name-status <last_commit>..HEAD 获取变更文件
4. 对每个文件：
   A (add)     → ruminate ingest 该文件
   M (modify)  → ruminate ingest 该文件（会更新已有 summary page）
   R (rename)  → ruminate ingest 新路径文件（LLM 分析产生相似结果，entity/concept 合并逻辑自动去重）
   D (delete)  → 不删除 wiki 内容，记录到 log.md，在对应 summary page 追加 "⚠️ Source removed" 标记
5. 更新 sync_state 中的 last_synced_commit
6. 输出同步摘要：N files ingested, M files deleted (kept in wiki)
```

#### 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 状态存储 | `wiki/.ruminate/sync_state.json` | 属于 wiki 的本地状态，不需要污染源仓库 |
| 删除处理 | **不删除 wiki 内容**，只标记 | 知识已被提取并合并到 entity/concept 页面，删除源文件 ≠ 知识失效 |
| 重命名处理 | 当作新文件 ingest | LLM 分析会产生相似结果，entity/concept 合并逻辑会自动去重 |
| 批量 vs 逐个 | 逐个文件调用 ingest | 复用现有管线，每个文件独立 LLM 分析 + git commit |

#### sync_state.json 格式

```json
{
  "sources": {
    "/home/zhangjie/notes": {
      "last_synced_commit": "abc123def456",
      "last_synced_at": "2026-07-06T10:30:00Z"
    },
    "/home/zhangjie/blog-ideas": {
      "last_synced_commit": "789xyz",
      "last_synced_at": "2026-07-05T14:00:00Z"
    }
  }
}
```

支持多个源仓库分别追踪各自的同步状态。

---

### `ruminate hook install`

```
ruminate hook install [--repo <path>] [--type post-commit]
ruminate hook uninstall [--repo <path>]
```

#### install 行为

在 `<repo>/.git/hooks/post-commit` 写入如下脚本：

```bash
#!/bin/sh
# Installed by ruminate hook install
# Calls ruminate sync to update the knowledge base after each commit
ruminate sync --repo "$(git rev-parse --show-toplevel)"
```

- 如果 hook 文件已存在（非 ruminate 安装的），报错并提示用户手动合并。
- 通过标记注释（`# Installed by ruminate hook install`）识别是否由 ruminate 管理。

#### uninstall 行为

删除由 ruminate 安装的 hook 文件。仅当文件包含 ruminate 标记注释时才删除，避免误删用户自定义 hook。

---

## 待决策问题

### 1. 性能：每次 commit 都触发 LLM 调用？

每次 `ruminate sync` 对每个变更文件走完整 ingest 管线（含 LLM 调用）。如果一个 commit 改了 5 个文件，就有 5 次 LLM 请求。

**选项：**
- **A**：接受，逐个文件处理（简单，正确）
- **B**：攒一批变更文件后合并为一次 LLM 调用（快但影响分析质量）
- **C**：默认手动 `ruminate sync`，hook 只是可选安装

**建议**：先用 A（简单正确），如果性能成为问题再优化。Hook 安装作为可选项。

### 2. 删除策略细化

源文件被删除时，它在 wiki 中可能创建了：
- 1 个 summary page（只属于这个源）
- N 个 entity page（可能被其他源引用）
- M 个 concept page（可能被其他源引用）

**当前建议**：
- entity/concept 页面：不删除（有其他源的引用）
- summary 页面：保留但追加 "⚠️ Source removed" 标记
- log.md：记录删除事件

**需要确认**：summary 页面是保留（带标记）还是直接删除？

### 3. Hook 类型选择

**选项：**
- **post-commit**：每次 commit 后触发。立即反馈，但多次 commit 会频繁触发 LLM。
- **手动命令**：用户自己跑 `ruminate sync`。灵活但需要记住。
- **两者结合**（推荐）：默认手动，hook 作为可选加速方式。

### 4. `--source-type` 的处理

`ingest` 需要 `-t`（article/paper/note/book）。`sync` 时如何确定每个文件的类型？

**选项：**
- **A**：统一用 `note`（默认值，最简单）
- **B**：根据目录名推断（如 `papers/` → paper，`articles/` → article）
- **C**：在源仓库放一个 `.ruminate-sync.yaml` 配置文件，映射路径模式到类型

**建议**：先实现 A（默认 `note`），后续按需支持 B 和 C。

---

## 实现计划

### 新增文件

```
internal/
├── cmd/
│   ├── sync.go           # ruminate sync 命令
│   └── hook.go           # ruminate hook install/uninstall 命令
├── sync/                  # 同步引擎
│   ├── doc.go            # 包文档
│   ├── engine.go         # SyncEngine：diff 计算、状态管理、文件分类处理
│   └── engine_test.go    # 测试
```

### 依赖

- `internal/gitwrap/`：扩展 `Git` 结构体，增加 `DiffNameStatus` 方法
- `internal/ingest/`：复用 `Engine.Ingest` 方法
- `internal/wiki/`：通过 `Manager` 访问 wiki 状态

### 任务拆分（建议优先级）

1. **扩展 `gitwrap.Git`**：增加 `DiffNameStatus(base, head string)` 方法，支持 `--name-status` 输出解析
2. **实现 `sync.Engine`**：状态管理（读/写 sync_state.json）、diff 解析、文件分类处理
3. **实现 `ruminate sync` 命令**：CLI 参数、调用 sync.Engine
4. **实现 `ruminate hook install/uninstall`**：hook 脚本生成和清理
5. **测试和文档**
