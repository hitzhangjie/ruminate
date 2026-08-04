# Agent 探索：ReAct、工具组合与代码理解

> 本文档回答：若允许 agent 探索（含读原文、读代码），应如何实现？
> 结论：**内嵌 ReAct**；读 wiki/raw/源文件是一等工具；代码侧默认 **rg/grep + tree-sitter**（多语言、零 LSP）；gopls/LSP 为可选增强。
> 最后更新：2026-08-04
>
> 依赖：[108-dual-truth-and-layered-retrieval.md](108-dual-truth-and-layered-retrieval.md)

---

## 一、核心立场（先给结论）

1. **提炼与探索解耦**  
   - Ingest：有损压缩 → 高密度 Wiki  
   - Query/Agent：可回退的证据探索 → 回答可溯源  

2. **通用探索用 ReAct，够用且不复杂**  
   - `Thought → Action(tool) → Observation` + 预算  
   - 产品形态：`ruminate ask --agent`（或 `ruminate agent`）  

3. **读代码默认不依赖 gopls**  
   - **P0 代码工具**：`file_grep`（rg）+ **tree-sitter**（大纲、定义候选、**包围函数/块**）+ `file_read`  
   - 对知识库场景：定位符号、读实现上下文，通常够用  
   - **gopls / 其他 LSP-MCP**：可选 P1+，要类型精确、跨包真·定义/引用时再开  

4. **组合优于重造类型系统**  
   - tree-sitter 是句法层，不是类型检查器；不假装跨 module 语义  
   - 外部 coding agent 仍可并行使用  

```text
┌──────────────────────────────────────────────────────────────┐
│            Control Plane：ReAct loop（ruminate 内嵌）          │
│         Thought → Action → Observation → … → Final Answer      │
└────────────────────────────┬─────────────────────────────────┘
                             │ 内置 tools（默认）
                             ▼
┌────────────────────────────────────────────────────────────┐
│ Knowledge          │  Evidence/Code（默认，无 LSP）           │
│ wiki_* / raw_*     │  file_grep (rg)                         │
│                    │  ast_outline / symbol_search (tree-sitter)│
│                    │  read_enclosing / file_read               │
│                    │  roots: wiki, raw, code                   │
└────────────────────────────────────────────────────────────┘
                             │ 可选
                             ▼
              gopls / rust-analyzer / … (LSP-MCP)
```

---

## 二、为什么「单轮 RAG」不够，需要 Agent

当前 `ask`：

```text
检索 top-N → 一次塞进 prompt → 生成答案
```

失败模式（企业常见）：

| 场景 | 单轮 RAG 的问题 | Agent 怎么做 |
|------|-----------------|--------------|
| 词不达意 | 一次 expansion 仍偏 | 读结果后改写 query 再搜 |
| 消歧 | 「元宝」货币 vs 猫 | 列出候选问用户，或读更多上下文 |
| 蒸馏丢失 | Wiki 没写默认超时 | 打开 raw / 源码核对 |
| 符号定义 | 文本里只有名字 | `go-to-definition` / 索引查询 |
| 跨层推理 | 架构决策在 wiki，实现在 repo | 先 wiki 定方向，再代码验证 |

Agent 的本质不是「更强的 embedding」，而是：

> **Thought → Action → Observation** 闭环，直到证据够或确认不够。

这与 [105](105-iterative-retrieval.md) 的 eager 多路不同：eager 是并行召回技巧；ReAct 是 **带状态的探索策略**。

---

## 三、选定方案：内嵌 ReAct（主路径）

> ReAct 足够覆盖「先查 wiki → 不够读 raw → rg/tree-sitter 定位代码 → 读包围作用域」；实现复杂度可控。

### 3.1 为什么选 ReAct 而不是更重的 agent 框架

| 选项 | 评价 |
|------|------|
| 单轮 RAG（当前 ask） | 快，但无法消歧/回退 |
| **ReAct tool loop** | **首选**：实现简单、可测、可加预算 |
| Plan-and-Execute / 多 agent | 企业编排可后做，P0 过重 |
| 只依赖外部 Claude Code | 很好的 **补充**，不能替代 CLI 内闭环 |

### 3.2 运行时（结构化 ReAct）

```text
state = { question, transcript[], evidence[], budget, roots }

loop until budget exhausted or done:
  1. LLM 输出其一：
       { "thought": "...", "action": "<tool>", "args": { ... } }
     或
       { "final_answer": "...", "references": [ ... ] }
  2. final_answer → 结束
  3. 校验 tool 名与 args；执行 → observation（截断）
  4. 将 (thought, action, observation) 追加 transcript
  5. 扣减 budget（steps / tokens / wall_time）
```

实现要点：

- **强制 JSON / tool-call schema**（不要靠自由文本解析 “Action:”）  
- **默认只读**；`--save` 才写 synthesis  
- **每步 trace**（复用 `-v` span）：tool 名、路径、耗时、observation 字节数  
- System prompt 写清：先 L1 wiki，不足再 raw；实现以源码为准；tree-sitter/rg 为句法/词法线索，非类型保证

伪代码量级（便于评估工作量）：

```go
// internal/agent/react.go — 示意
for step := 0; step < maxSteps; step++ {
    decision, err := llm.Decide(ctx, sys, transcript, tools.Schema())
    if decision.Final != "" {
        return decision.Final, decision.Refs, nil
    }
    obs, err := tools.Exec(ctx, decision.Action, decision.Args) // 含 path sandbox
    transcript = append(transcript, Turn{Thought: decision.Thought, Action: ..., Obs: obs})
}
```

### 3.3 内置工具目录（ReAct 直接可调）

#### A. Knowledge（Wiki + Raw）

> **Agent 工具 ≠ 单轮 ask 管线。** Agent 自己做多步决策，工具应是廉价导航原语。
> 默认策略：`wiki_index`（目录）→ `wiki_read` 下钻；需要关键词时用 `wiki_search`（**仅 FTS/BM25**，无 embedding / 无 query expansion / 无 MMR）。
> Hybrid Search + effort 只属于非 agent 的 `ask` 路径。

| Tool | 作用 | 实现 |
|------|------|------|
| `wiki_index` | 读 catalog（index.md）；可选 filter | `ReadByPath("index.md")` |
| `wiki_search` | L1 **关键词**检索 | `SearchKeyword`（FTS only） |
| `wiki_read` | 读 wiki 页全文 | `ReadByPath` |
| `wiki_links` | 出入链 | WikiLink 索引 |
| `raw_list_sources` | 页的 contributing sources | frontmatter（108） |
| `raw_read` | 读 raw 全文 / 按 heading | `raw/` |
| `raw_search` | L2 检索 | `raw_fts`（独立 FTS，非 hybrid） |

#### B. 通用文件 + 句法结构（P0 默认代码能力）

| Tool | 作用 | 实现 |
|------|------|------|
| `file_read` | 按 path + offset/limit 读 | 文件系统；roots 沙箱 |
| `file_grep` | 字面/正则搜索 | **rg**（或内置 grep） |
| `list_dir` | 列目录 | 浅层、限条目 |
| `ast_outline` | 文件/目录符号大纲 | **tree-sitter** |
| `symbol_search` | 按名找定义候选（func/type/class…） | tree-sitter query + 可选 rg 补全 |
| `read_enclosing` | 命中行/字节偏移 → **包围的 function/block** | tree-sitter 上卷父节点 |

**Roots（沙箱）**：

```yaml
agent:
  roots:
    - ${wiki}/wiki
    - ${wiki}/raw
    - /path/to/service
  max_read_bytes: 65536
  max_steps: 12
  # tree-sitter grammars: go, python, …（按需内嵌或插件）
```

**读原文** = raw 上的 raw_*/file_*；**读代码** = code root 上的 grep + tree-sitter + read。同一套哲学，**默认无 LSP 进程**。

#### C. 语言语义 LSP（可选，非默认）

| Tool | 后端 | 何时需要 |
|------|------|----------|
| `lsp_definition` / `references` / `hover` | gopls MCP、rust-analyzer… | 跨包真定义、接口实现、类型精确签名 |

见 §3.5。P0 **可以不实现**。

### 3.4 默认代码路径：rg + tree-sitter

#### 为什么可以不上 gopls

| 维度 | rg + tree-sitter | gopls / LSP |
|------|------------------|-------------|
| 依赖 | 库 + grammar，进程内 | 外挂语言服务器、版本、工作区加载 |
| 多语言 | 加 grammar 即可统一 API | 每种语言一个 server |
| 包围函数 / 块 | **原生擅长** | 靠 range / documentSymbol |
| 同文件结构大纲 | ✅ | ✅ |
| 跨包 go-to-def、实现、类型 | ❌ 或启发式 | ✅ |
| 适用 Ruminate | **只读探索 / 核对 wiki** | 深 IDE 级语义 |

知识库问答里，Agent 多数时候需要的是：

1. 名字在哪出现（rg）  
2. 哪个 `func`/`type` 声明（tree-sitter）  
3. **把该函数体读出来**（enclosing），而不是整文件  

这三步 **不需要类型检查器**。跨 module 的「真正定义」偶发需要时，再开 LSP 或让人用 IDE。

#### 「查符号」ReAct 轨迹（默认）

```text
Thought: wiki 有无业务说明
Action: wiki_search("Reconcile")
Obs: concept + sources

Thought: 读 ADR
Action: raw_read(...)

Thought: 代码里定义在哪
Action: symbol_search(name="Reconcile", langs=["go"])
   或 file_grep("func Reconcile")
Obs: path:line 候选列表

Thought: 只读该函数，不读整文件
Action: read_enclosing(path=..., line=...)
Obs: 函数体源码

Final: wiki/raw 意图 + 代码实现片段 + references
     （可注明：基于句法定位，非 gopls 类型解析）
```

#### tree-sitter 能力边界（必须写进 prompt / 文档）

- **能**：按语法节点找 `function_declaration`、`type_declaration`、class/method；上卷 enclosing；多语言同一 tool 形状  
- **不能**：分辨同名不同包、接口满足关系、泛型实例化、build tag 下「当前真正编译的定义」  
- **同名冲突**：返回多个候选，由 ReAct 再读或问用户；禁止假装唯一  

#### 实现提示（Go 栈友好）

- 库：如 `github.com/smacker/go-tree-sitter` 或维护中的等价绑定 + 官方 grammars（go/python/…）  
- 预编译 grammar，避免运行时拉网络  
- `symbol_search`：对 code roots 做（可缓存的）轻量遍历，或「先 rg 文件列表再 parse」两段式，避免全仓无脑 parse  
- `read_enclosing`：parse 单文件 → 含 line 的最深 statement → 上卷到 function/method/block  

### 3.5 可选：组合 gopls / LSP-MCP

需要类型级精度时再启用（非 MVP）：

- [gopls MCP](https://go.dev/gopls/features/mcp)：`gopls mcp` 或 `-mcp.listen`  
- 配置：`agent.lsp_mcp.gopls: { command: gopls, args: [mcp] }`  
- 与 tree-sitter **并存**：LSP 命中后仍可用 `read_enclosing` 取上下文  
- 默认关闭，避免强依赖 go 工具链与模块下载行为

### 3.5 停止与预算

| 预算 | 默认建议 | 说明 |
|------|----------|------|
| max_steps | 32（可 `--max-steps`） | 防死循环；agent 用步数换精度 |
| max_read_bytes | 单次 32–64KB | 大文件强制 range |
| transcript compaction | 最近 4 步全文，更早 obs 截断 | 防上下文膨胀导致 JSON 解析失败 |
| wall_time | 30–120s | CLI 友好 |
| token 可见性 | 每步 + 汇总 | `-v` 展示 prompt→completion；无 usage 时回退 chars 估计 |

停止条件：足够 references / 连续 2 步无新信息 / 用户中断 / 预算耗尽（返回已有证据 + 说明未完成）。

### 3.6 记忆与写回

- **会话内**：transcript + evidence[]  
- **跨会话**：仅 `--save` 写 synthesis（卡氏复利）  
- **禁止**探索中静默改 entity/concept（与 ingest/lint 分离，见 100）

---

## 四、与外部通用 Agent 的关系（并存，非互斥）

内嵌 ReAct 解决「ruminate 自己会探索」。  
外部 agent（Claude Code、Codex…）仍可通过 skills / CLI / **Ruminate MCP** 把本库当 Knowledge Plane——尤其在要 **改代码、跑测试、大规模编辑** 时，宿主更合适。

### 4.1 三条用法

| 用法 | 何时 |
|------|------|
| **A. `ruminate ask --agent`** | 知识库问答、核对 raw、rg+tree-sitter 探代码 |
| **B. 外部 agent + skill** | 人已在 IDE/终端 agent 里，顺带查 wiki |
| **C. 外部 agent + ruminate MCP（+ 可选 gopls）** | 知识层对外；语义 LSP 由宿主自挂 |

### 4.2 Ruminate 作为 MCP Server（对外）

| MCP Tool | 映射 |
|----------|------|
| `search_wiki` / `read_wiki_page` | L1 |
| `search_raw` / `read_raw` | L2 |
| `list_page_sources` | sources |
| （可选）不暴露写，或 `save_synthesis` 需 confirm | |

对外 MCP 做 Knowledge；代码句法由内嵌 tree-sitter 或宿主自己的工具完成。

### 4.3 职责边界（修订）

| 职责 | Ruminate ReAct | 外部 Agent | gopls（可选） |
|------|----------------|------------|----------------|
| Ingest / lint / wiki | ✅ | 可触发 | — |
| 多步只读探索 | ✅ | 可替代 | — |
| 读 raw / 源文件 | ✅ | ✅ | — |
| 句法符号 / enclosing | ✅ tree-sitter | ✅ | — |
| 跨包类型级定义 | 可选调用 | 常自带 | ✅ |
| 改业务代码 | ❌ 默认 | ✅ | 可能建议 edit |
| 写回 synthesis | `--save` | 调 CLI | — |

---

## 五、「理解代码」到底指什么

业界容易把「理解」神话。可操作的分解：

### 5.1 四层理解

| 层 | 问题 | 机制 | Ruminate 角色 |
|----|------|------|----------------|
| **Lexical** | 哪里出现这个字符串？ | **rg** / FTS | ✅ P0 |
| **Structural（句法）** | 函数/类型在哪、包围块 | **tree-sitter** | ✅ P0 默认 |
| **Structural（语义）** | 跨包真定义、类型 | LSP（可选） | P1+ |
| **Behavioral** | 运行时做什么？ | 读实现 | 只读为主 |
| **Conceptual** | 为什么这样设计？ | wiki + raw | ✅ 主场 |

企业问答常常混层：

- 「`Reconcile` 大概怎么实现？」→ tree-sitter 定位 + `read_enclosing`  
- 「接口 X 的哪个类型满足？」→ 需要 LSP 或人工；tree-sitter 只能给候选  
- 「为什么这样设计？」→ wiki/ADR  

### 5.2 符号怎么找（默认：rg + tree-sitter）

**P0 标准路径（不上 gopls）：**

1. `file_grep` / `symbol_search` → 候选 `path:line`  
2. `read_enclosing` → 只取函数/类型声明体  
3. 多候选时再读或消歧  

**P1 可选：gopls/LSP** — 跨包、类型、实现关系。

**反模式**：

- 源码全量向量化当唯一真相  
- wiki 符号页不留 `file` 指针  
- 用 tree-sitter 结果写成「类型系统保证」  
- 为「像 IDE」强绑 gopls 当 MVP 依赖  

### 5.3 rg + tree-sitter 对 Ruminate 够不够？

**够。且定为默认代码智能。**

| 需求 | rg | tree-sitter | 合起来 |
|------|-----|-------------|--------|
| 字符串/配置/日志 | ✅ | — | 词法 |
| 函数/类型声明候选 | 启发式 | ✅ 句法 | 定位 |
| **只读包围函数，不读整文件** | — | ✅ | 省 token（你关心的细节） |
| 同文件结构 | — | ✅ outline | 导航 |
| 跨包精确 def/impl | ❌ | ❌ | 需 LSP 或接受候选 |
| 设计叙事 | ❌ | ❌ | Wiki/raw |

与完整 coding agent 的差距主要在 **写代码闭环与类型级重构**，不是「能不能读懂一段实现」。

**结论**：P0 = **ReAct + wiki/raw + rg + tree-sitter（含 read_enclosing）**。  
gopls = 可选增强，不是门槛。

### 5.4 多语言：默认 tree-sitter 统一，LSP 按需

```text
Agent（语言无关）
  ├── L0 rg + read          全语言
  ├── L1 tree-sitter        加 grammar → 统一 symbol/enclosing API   ← 默认
  └── L2 LSP/MCP            按项目启用                              ← 可选
```

| 扩语言 | 怎么做 |
|--------|--------|
| 新语言只要「能搜、能抽函数」 | 加 tree-sitter grammar + query 映射（func/class 节点名） |
| 新语言要 IDE 级跳转 | 挂对应 language server（与 gopls 同插槽） |
| 无 grammar | 仅 rg + 按行读，质量降级 |

业界对照：Aider 等以 tree-sitter repo-map 为主；Claude Code 等大量依赖 grep+read；LSP 是增强层而非人人标配。  
Ruminate 对齐 **Aider 式默认（句法）**，而不是 **IDE 式默认（gopls）**。

### 5.5 若要对代码做 ingest，正确姿势

代码可以进入 Ruminate，但应作为 **Conceptual + 指针**，不是替代 Code Plane：

```yaml
# wiki/entities/Reconcile.md
---
title: Reconcile
type: entity
code_anchors:
  - repo: services/controller
    path: pkg/reconcile/loop.go
    symbol: Reconcile
    kind: definition
sources:
  - path: raw/adr/001-reconcile-design.md
---

# 业务含义与设计取舍（Synthesis）
...
# 实现指针（Evidence）
- 定义：`pkg/reconcile/loop.go`（以 LSP/源码为准）
```

Ingest 管道产出的是 **可导航的概念图 + 锚点**；**签名与行为以代码为准**，agent 回答前应 verify 锚点是否仍存在（文件移动则 stale）。

---

## 六、端到端场景

### 场景 1：纯知识库（个人研究）

```text
ask --evidence auto
  → L1 wiki
  → 不足则 raw_read(sources)
  → 回答 + 引用
```

可不启用外部 agent。

### 场景 2：企业文档 + 原文核对

```text
外部 Agent + ruminate MCP
  → search_wiki
  → list_page_sources → read_raw
  → 若政策条款需精确原文：引用 raw 段落
```

### 场景 3：架构问题 + 代码验证（内嵌 ReAct，无 gopls）

```text
ruminate ask --agent "Reconcile 会不会阻塞？"
  → wiki_search / wiki_read
  → raw_read(ADR)
  → symbol_search / file_grep
  → read_enclosing(函数体)
  → file_grep(配置默认值)
  → Final + references；可选 --save
```

### 场景 4：只有代码、几乎没有 wiki

```text
配置 code root
  → ReAct：file_grep + tree-sitter + read_enclosing
  → 稳定结论再 ingest 进 wiki
  → 之后「先 wiki 后代码」
```

冷启动期直读代码是正常的。

---

## 七、产品落点（与 CLI 的关系）

### 7.1 短期（P0）

| 能力 | 说明 |
|------|------|
| frontmatter `sources` | raw 回退 |
| `ask --evidence wiki\|auto\|raw` | 无多步分层 |
| **ReAct + wiki/raw + file_grep + tree-sitter** | 含 `read_enclosing` / `symbol_search` |
| `agent.roots` 沙箱 | 限制可读路径 |

### 7.2 中期

| 能力 | 说明 |
|------|------|
| 更多 grammar（py/ts/…） | 同一 tool API |
| 轻量符号缓存 / repo outline | 加速 cold search |
| `ruminate mcp` + skill | 对外 Knowledge |
| `ask --json` | 结构化 references |

### 7.3 长期（可选）

| 能力 | 说明 |
|------|------|
| gopls / 多语言 LSP-MCP | 类型级精度 |
| code_anchors + stale lint | 锚点失效 |
| 域 schema / 授权 run_tests | 扩展 |

**刻意不做（默认）**：强依赖 gopls、自研类型系统、默认写代码。

---

## 八、风险与原则清单

1. **蒸馏有损** → Evidence 回退；标注 Wiki vs raw vs code  
2. **句法 ≠ 语义** → tree-sitter 结果不得宣传为类型保证  
3. **同名冲突** → 多候选；禁止假装唯一定义  
4. **幻觉 tool** → 只注入真实 observation  
5. **权限** → roots；默认只读  
6. **成本** → max_steps；enclosing 优先于整文件  
7. **可选 LSP** → 需要时再开，不挡 MVP  

---

## 九、决策摘要

| 问题 | 决策 |
|------|------|
| Agent？ | **要**；ReAct |
| 读原文/代码？ | **能**；roots 沙箱 |
| 代码智能默认？ | **rg + tree-sitter**（含 enclosing） |
| 必须上 gopls 吗？ | **否**；可选 P1+ |
| 符号怎么找？ | symbol_search / grep → read_enclosing |
| 多语言？ | **加 grammar** 统一 API；LSP 按需 |
| 与卡氏？ | 编译 Wiki + Evidence 回退 + 可导航探索 |

---

## 十、参考

- [108-dual-truth-and-layered-retrieval.md](108-dual-truth-and-layered-retrieval.md)
- [105-iterative-retrieval.md](105-iterative-retrieval.md)
- [106-small-to-big-retrieval.md](106-small-to-big-retrieval.md)
- [100-ingest-lint-separation.md](100-ingest-lint-separation.md)
- [llm_wiki.md](../llm_wiki.md)
- [Tree-sitter](https://tree-sitter.github.io/tree-sitter/)
- Aider repo-map（tree-sitter 符号图思路）
- [Gopls MCP](https://go.dev/gopls/features/mcp) — 可选增强
- Yao et al., ReAct: Synergizing Reasoning and Acting in Language Models
