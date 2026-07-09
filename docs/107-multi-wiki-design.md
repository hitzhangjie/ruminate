# 多知识库支持设计

> 本文档描述 Ruminate 从单知识库演进到多知识库的完整方案。
> 最后更新：2026-07-09

---

## 一、动机

当前 Ruminate 只支持单一知识库（`Config.WikiPath` 是单个字符串），这在以下场景中不够用：

- 用户同时维护多个知识库（如"工作笔记"和"个人研究"）
- 不同知识库使用不同的 LLM 模型或参数
- 用户希望在不同知识库之间切换，而非每次修改配置

## 二、核心约束

1. **统一管理目录**：所有知识库存放在 `$HOME/.ruminate/` 下，每个知识库是一个独立子目录
2. **名称即标识**：每个知识库由一个唯一的 `name` 标识，目录名 = wiki 名
3. **约定优于配置**：`~/.ruminate/<name>` 就是路径，无需额外指定
4. **P0 不支持自定义路径**：等真实用户反馈再考虑开放

## 三、目录结构

```
$HOME/.ruminate/
├── config.yaml                # 全局配置：wiki 注册表 + 默认 wiki + serve 配置
├── my-notes/                  # wiki "my-notes"
│   ├── config.yaml            # per-wiki 配置：LLM、embedding、verbose
│   ├── raw/
│   ├── wiki/
│   ├── index.md
│   ├── log.md
│   ├── schema.md
│   └── .ruminate/
│       ├── fts.db
│       └── sync_state.json
└── research/                  # wiki "research"
    ├── config.yaml
    ├── raw/
    ├── wiki/
    └── ...
```

## 四、配置文件

### 4.1 配置文件搜索

不再支持 viper 的多路径合并。按优先级搜索以下文件，找到即停（无覆盖关系）：

| 优先级 | 文件 | 说明 |
|--------|------|------|
| 1 | `$HOME/.ruminate/<wikiname>/config.yaml` | per-wiki 配置 |
| 2 | `$HOME/.ruminate/config.yaml` | 全局配置 |

每个配置文件包含各自层面的**完整必要配置**，不存在跨文件覆盖/合并。

### 4.2 全局配置 (`$HOME/.ruminate/config.yaml`)

```yaml
# 默认知识库名称。不指定 --wiki 时使用此项。
# 如果只注册了一个 wiki，自动视为默认（此字段可选）。
default_wiki: "my-notes"

# 注册的知识库列表。每次 ruminate init 在此追加一项。
wikis:
  - name: "my-notes"
  - name: "research"

# HTTP server 配置（对所有 wiki 生效）
serve:
  host: "127.0.0.1"
  port: 8420
```

### 4.3 Per-wiki 配置 (`$HOME/.ruminate/<name>/config.yaml`)

```yaml
# LLM 推理配置（该知识库专用）
llm:
  provider: "ollama"
  base_url: "http://localhost:11434"
  model: "gpt-oss:20b"
  temperature: 0.3
  max_input_tokens: 131072

# Embedding 配置（该知识库专用）
embedding:
  provider: "ollama"
  base_url: "http://localhost:11434"
  model: "nomic-embed-text"

# 详细日志（可覆盖全局默认）
verbose: false
```

### 4.4 字段归属

| 字段 | 归属 | 说明 |
|------|------|------|
| `default_wiki` | 全局 | 默认知识库名称 |
| `wikis` | 全局 | 已注册知识库列表 |
| `serve` | 全局 | HTTP server 配置 |
| `llm` | per-wiki | LLM 推理配置 |
| `embedding` | per-wiki | 嵌入模型配置 |
| `verbose` | per-wiki | 详细日志开关 |

## 五、配置命令

### 5.1 子命令结构

```
ruminate config
├── show          显示配置
├── edit          编辑配置
├── list          列出所有知识库
└── set           设置默认字段 (default-wiki, default-llm, default-embedding)
```

### 5.2 命令行为

```bash
# 展示全局配置
ruminate config show

# 展示 per-wiki 配置
ruminate config show --wiki my-notes

# 编辑全局配置（打开 $EDITOR 或通过 --set）
ruminate config edit
ruminate config edit --set serve.port=8430

# 编辑 per-wiki 配置
ruminate config edit --wiki my-notes
ruminate config edit --wiki my-notes --set llm.model=qwen3:32b

# 列出所有知识库
ruminate config list
# 输出示例：
#   * my-notes  (~/.ruminate/my-notes)    [default]
#     research   (~/.ruminate/research)

# 设置默认知识库
ruminate config set default-wiki research
```

### 5.3 分层约束

- `ruminate config edit --wiki <name>` **只能修改 per-wiki 字段**（llm、embedding、verbose）
- `ruminate config edit`（无 `--wiki`）**只能修改全局字段**（default_wiki、wikis、serve）
- 尝试在 per-wiki 上下文中修改全局字段（或以其他方式）应明确报错

## 六、init 命令

### 6.1 新签名

```
ruminate init <wikiname>
```

### 6.2 行为

1. 校验 `wikiname` 是否合法（字母数字下划线连字符，不能为空）
2. 检查 `$HOME/.ruminate/<wikiname>` 是否已存在
3. 创建 wiki 目录结构（raw/、wiki/、index.md、log.md、schema.md、.ruminate/）
4. 生成默认 `$HOME/.ruminate/<wikiname>/config.yaml`
5. 在全局配置的 `wikis` 列表中追加一项
6. 如果这是唯一的知识库，自动设为 `default_wiki`
7. 如果全局配置不存在（首次 init），自动创建

### 6.3 示例

```bash
ruminate init my-notes
# 输出：
#   Wiki "my-notes" initialized at ~/.ruminate/my-notes
#   Set as default wiki (only wiki so far).
#
#   Directory structure created:
#     raw/          — source materials
#     wiki/         — generated wiki pages
#     index.md      — human-readable page index
#     log.md        — operations log
#     schema.md     — wiki structure and conventions
#     config.yaml   — wiki-specific configuration
#     .ruminate/    — internal state (FTS5 index)

ruminate init research
# 输出：
#   Wiki "research" initialized at ~/.ruminate/research
#   Use "ruminate config set default-wiki research" to switch default wiki.
```

## 七、--wiki 参数

### 7.1 Root Persistent Flag

`--wiki` 作为 root command 的 persistent flag，所有子命令自动继承：

```bash
ruminate --wiki research ask "什么是 RAG?"
ruminate --wiki my-notes ingest paper.pdf -t paper
ruminate find "kubernetes"                    # 使用默认 wiki
```

### 7.2 Wiki 解析优先级

每个命令启动时，按以下优先级确定目标 wiki：

1. `--wiki <name>` flag（显式指定）
2. 全局配置中的 `default_wiki`
3. 如果只注册了一个 wiki，自动视为默认
4. 如果无法确定（多个 wiki 且无默认），报错提示用户选择

```go
func resolveWiki(name string) (*WikiRef, error) {
    global := loadGlobalConfig()

    // 1. explicit flag
    if name != "" {
        for _, w := range global.Wikis {
            if w.Name == name {
                return &WikiRef{Name: name, Path: w.path()}, nil
            }
        }
        return nil, fmt.Errorf("wiki %q not found. Known wikis: %v", name, wikiNames(global.Wikis))
    }

    // 2. default_wiki
    if global.DefaultWiki != "" {
        for _, w := range global.Wikis {
            if w.Name == global.DefaultWiki {
                return &WikiRef{Name: w.Name, Path: w.path()}, nil
            }
        }
    }

    // 3. only one wiki — auto-default
    if len(global.Wikis) == 1 {
        return &WikiRef{Name: global.Wikis[0].Name, Path: global.Wikis[0].path()}, nil
    }

    // 4. ambiguous
    return nil, fmt.Errorf(
        "multiple wikis found and no default set. Use --wiki or set a default:\n  ruminate config set default-wiki <name>",
    )
}
```

## 八、各命令适配

### 8.1 ingest

```
ruminate ingest <file|url>            # 导入到默认 wiki
ruminate --wiki research ingest ...   # 导入到指定 wiki
```

### 8.2 ask

```
ruminate ask "问题"                   # 查询默认 wiki
ruminate --wiki research ask "问题"   # 查询指定 wiki
```

### 8.3 find

```
ruminate find "关键词"                # 搜索默认 wiki
ruminate --wiki research find "关键词" # 搜索指定 wiki
```

### 8.4 sync

```
ruminate sync                         # 同步默认 wiki（或唯一 wiki）
ruminate sync --wiki research         # 同步指定 wiki
ruminate sync --all                   # 同步所有注册的 wiki
```

`--all` 和 `--wiki` 互斥，同时指定时报错。

### 8.5 lint

```
ruminate lint                         # 巡检默认 wiki
ruminate lint --wiki research         # 巡检指定 wiki
```

### 8.6 hook install

hook 安装时需要知道目标 wiki 名，写入 hook 脚本：

```bash
# hook 脚本中写入：
ruminate sync --wiki my-notes --repo "$(git rev-parse --show-toplevel)"
```

```
ruminate hook install --wiki my-notes [--repo ...]
```

### 8.7 serve

```
ruminate serve                        # 启动服务（覆盖所有 wiki）
```

serve 命令**不接受 `--wiki` 参数**——它同时服务所有注册的知识库。

Web 界面行为：
- 侧边栏列出所有 wiki，当前选中项高亮
- `/wiki/<name>/chat` — 在指定 wiki 上下文中问答
- `/wiki/<name>/browse` — 浏览指定 wiki 的页面
- 回答时显示 "@<wiki-name>" 标签标明上下文

### 8.8 reindex

```
ruminate reindex                      # 重建默认 wiki 的索引
ruminate --wiki research reindex      # 重建指定 wiki 的索引
```

## 九、命令汇总

| 命令 | 需要 --wiki | 说明 |
|------|------------|------|
| `init <name>` | 不需要 | 创建新 wiki |
| `ingest <file>` | 可选 | 默认 wiki 或显式指定 |
| `ask <question>` | 可选 | 默认 wiki 或显式指定 |
| `find <keywords>` | 可选 | 默认 wiki 或显式指定 |
| `sync` | 可选 | 默认 wiki，`--all` 同步所有 |
| `lint` | 可选 | 默认 wiki 或显式指定 |
| `hook install` | **需要** | 必须指定所属 wiki |
| `reindex` | 可选 | 默认 wiki 或显式指定 |
| `serve` | 不支持 | 服务所有 wiki |
| `config show` | 可选 | 有/无 `--wiki` 展示不同层级配置 |
| `config edit` | 可选 | 有/无 `--wiki` 编辑不同层级配置 |
| `config list` | 不支持 | 列出所有 wiki |
| `config set` | 不支持 | 设置全局默认字段 (default-wiki, default-llm, default-embedding) |

## 十、Config 数据模型

### 10.1 Go 结构体

```go
// GlobalConfig 全局配置，存储在 $HOME/.ruminate/config.yaml
type GlobalConfig struct {
    DefaultWiki string      `yaml:"default_wiki"`
    Wikis       []WikiEntry `yaml:"wikis"`
    Serve       ServeConfig `yaml:"serve"`
}

type WikiEntry struct {
    Name string `yaml:"name"`
    // Path 由约定推导: $HOME/.ruminate/<Name>
    // P0 不支持自定义 Path，保留此注释以便未来扩展
}

// WikiConfig per-wiki 配置，存储在 $HOME/.ruminate/<name>/config.yaml
type WikiConfig struct {
    LLM       LLMConfig       `yaml:"llm"`
    Embedding EmbeddingConfig `yaml:"embedding"`
    Verbose   bool            `yaml:"verbose"`
}

// ServeConfig HTTP server 配置（全局）
type ServeConfig struct {
    Host string `yaml:"host"`
    Port int    `yaml:"port"`
}

// LLMConfig LLM 推理配置（per-wiki）
type LLMConfig struct {
    Provider       string  `yaml:"provider"`
    BaseURL        string  `yaml:"base_url"`
    Model          string  `yaml:"model"`
    Temperature    float64 `yaml:"temperature"`
    MaxInputTokens int     `yaml:"max_input_tokens"`
}

// EmbeddingConfig 嵌入模型配置（per-wiki）
type EmbeddingConfig struct {
    Provider string `yaml:"provider"`
    BaseURL  string `yaml:"base_url"`
    Model    string `yaml:"model"`
}
```

### 10.2 运行时 Config（组合后）

```go
// RuntimeConfig 是 GlobalConfig + 当前选中 WikiConfig 的组合，
// 保留了原有 Config 的字段以最小化下游改动。
//
// 每次命令执行时：先加载 GlobalConfig，解析目标 wiki，
// 再加载对应 WikiConfig，合并为 RuntimeConfig。
type RuntimeConfig struct {
    // 当前操作的 wiki 标识
    WikiName string
    WikiPath string // 推导: $HOME/.ruminate/<WikiName>

    // Per-wiki 配置
    LLM       LLMConfig
    Embedding EmbeddingConfig
    Verbose   bool

    // 全局配置（serve 需要）
    Serve ServeConfig

    // 全局配置（wiki 注册表，serve 需要）
    Wikis       []WikiEntry
    DefaultWiki string
}
```

## 十一、数据流

```
ruminate --wiki research ask "什么是 RAG?"
            │
            ▼
    ┌──────────────────┐
    │ root.PersistentPreRunE │  解析 --wiki flag
    └──────┬───────────┘
           │
           ▼
    ┌──────────────────┐
    │ resolveWiki()     │  1. 加载 GlobalConfig
    │                   │  2. 按优先级确定目标 wiki
    └──────┬───────────┘
           │
           ▼
    ┌──────────────────┐
    │ loadWikiConfig()  │  加载 $HOME/.ruminate/<name>/config.yaml
    └──────┬───────────┘
           │
           ▼
    ┌──────────────────┐
    │ RuntimeConfig     │  合并后的完整配置
    └──────┬───────────┘
           │
           ▼
    ┌──────────────────┐
    │ 子命令 RunE       │  ask / find / ingest / sync / lint
    └──────────────────┘
```

## 十二、迁移

### 12.1 从旧版本迁移

当前版本（单 wiki）的 `WikiPath` 可能是任意目录。多 wiki 方案不兼容旧配置。

**迁移策略**（手动 + 辅助命令）：

```bash
# 方案 A：用户手动迁移
mv ~/ruminate_wiki ~/.ruminate/my-notes
# 创建 ~/.ruminate/my-notes/config.yaml（复制旧的 .ruminate.yaml 中 LLM/embedding 部分）
ruminate config edit --wiki my-notes

# 方案 B：提供 import 子命令（P1）
ruminate init --import ~/ruminate_wiki my-notes
```

由于项目仍在 P0 阶段，没有稳定用户群，建议 P0 不做自动迁移，只在 release note 中说明。

### 12.2 废弃的搜索路径

以下路径不再搜索（与旧版不兼容）：

- `./.ruminate.yaml`（当前目录）
- `$HOME/.config/ruminate/config.yaml`
- `$HOME/.ruminate.yaml`

## 十三、实现任务

| 序号 | 任务 | 涉及文件 | 优先级 |
|------|------|---------|--------|
| 1 | 定义新的 Config 结构体（GlobalConfig、WikiConfig、RuntimeConfig） | `internal/config/config.go` | P0 |
| 2 | 实现 LoadGlobalConfig / LoadWikiConfig / Save* | `internal/config/config.go` | P0 |
| 3 | 重写 `ruminate init`（改为 `init <name>` 语义） | `internal/cmd/init.go` | P0 |
| 4 | `--wiki` persistent flag + resolveWiki | `internal/cmd/root.go` | P0 |
| 5 | 所有命令适配 RuntimeConfig | `internal/cmd/{ingest,ask,find,sync,lint,reindex}.go` | P0 |
| 6 | `ruminate config show/edit/list/set` | `internal/cmd/config.go` | P0 |
| 7 | `ruminate sync --all` | `internal/cmd/sync.go` | P0 |
| 8 | `ruminate hook install --wiki` | `internal/cmd/hook.go` | P0 |
| 9 | `ruminate serve` 多 wiki 支持 | `internal/cmd/serve.go` + `internal/serve/` | P1 |
| 10 | 更新测试 | 各 `*_test.go` | P0 |
| 11 | 更新架构文档 | `docs/2-architecture.md` | P1 |

## 十四、设计决策记录

1. **不开放自定义路径（P0）**：如果用户需要 wiki 放在特定磁盘位置，使用符号链接。自定义路径增加了"发现 wiki"的难度（需要维护 name→path 映射），且 P0 阶段无证据表明这是刚需。

2. **配置不做跨文件覆盖**：每个配置文件自包含，避免"这个值从哪个文件来的"心智负担。per-wiki 配置在 `init` 时自动生成完整默认值，用户只需按需修改。

3. **`--wiki` 做 persistent flag 而非子命令参数**：这样 `ruminate --wiki research ask "..."` 和 `ruminate ask --wiki research "..."` 都可以工作（cobra 的 persistent flag 也出现在子命令上），但定义只在一处。

4. **serve 不接 `--wiki`**：serve 是守护进程，理应服务所有 wiki。按 wiki 筛选是 Web UI 侧边栏的职责。
