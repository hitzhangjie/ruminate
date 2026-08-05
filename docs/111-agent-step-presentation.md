# Agent 执行过程呈现：Live TUI → 纯文本降级 → Web

> 本文档回答：ReAct agent 跑起来后，如何让人「扫得懂」而不是淹没在文本墙里？  
> 结论：**数据层** = `Step` + `OnProgress` + `OnStep`；**TTY 默认**对齐 Claude Code 风格的 progressive live view（spinner + 卡片）；非 TTY / `-v` 降级纯文本；Web 为后续折叠回放面。  
> 最后更新：2026-08-05  
>
> 依赖：[109-agent-exploration.md](109-agent-exploration.md)

---

## 一、背景与问题

可观测数据已经够用：

- 每步 `Thought → Action(+args) → Observation`
- `-v` 可附带 decide-round prompt 与 raw LLM response
- `OnStep` 解耦内核与展示

但「纯文本紧凑一行」仍然像 **日志墙的压缩版**，和 Claude Code / Grok 等成熟 agent CLI 的差距在于：

| 缺口 | 体感 |
|------|------|
| 无进行中态 | LLM/tool 长等待时终端空白 |
| 成功/失败视觉层级弱 | 扫读靠猜，不像「卡片时间线」 |
| 错误展开仍偏「日志块」 | 不像 coding agent 的 `⎿ ERROR` 行 |
| 无统一 chrome | 颜色、图标、缩进各自为政 |

**方向**：对齐 Claude Code / Grok 的 **信息架构**（摘要优先、进行中反馈、错误点状高亮），TTY 上用成熟终端样式库（Lip Gloss），而不是继续堆 `── Step N ──` 文本。

---

## 二、设计原则

1. **`agent` 零 UI 依赖**  
   只发事件：`OnProgress`（步中）+ `OnStep`（步末）。

2. **展示可插拔**  
   ```text
   agent.Run
      ├── OnProgress(Progress)   // decide | tool
      └── OnStep(Step)
             ├── agentview.View     // TTY 默认：live progressive UI
             ├── cmd.writeAgentStep // 非 TTY 紧凑 / -v 全量
             └── serve.SSEHub       // 未来 Web
   ```

3. **渐进披露**  
   默认卡片只露：工具名、关键参数、耗时、体量/token、错误首行。  
   Thought / 长 Observation 仅错误预览或 `-v`。

4. **TTY 增强、管道友好**  
   有 TTY → live view（spinner 用 `\r` 原地刷新）。  
   管道 / 非 TTY → 纯文本，无控制序列动画。  
   尊重 `NO_COLOR`；`FORCE_COLOR` 可强制色。

5. **CLI first, then Web**  
   本地调试默认就是 live view；Web 补回放与长 observation，不替代 CLI。

---

## 三、默认体验（TTY · Live View）

实现：`internal/ui/agentview`（[Lip Gloss](https://github.com/charmbracelet/lipgloss) + 自研 spinner，**未**上全屏 Bubble Tea）。

### 进行中

```text
  ⠋ Thinking… (step 4)
  ⠙ wiki_search · "分布式 系统 历史"
```

- `ProgressDecide` → Thinking spinner  
- `ProgressTool` → 切换为工具名 + 参数摘要  

### 步完成（卡片）

```text
  ● wiki_index · filter="Distributed"  11.132s
    ⎿  62B · 2136→66 tok

  ✗ wiki_search · "分布式 系统 历史"  45.127s
    ⎿  searching FTS with snippets: SQL logic error: fts5: syntax error near ""系统""
       try Chinese query…

  ◆ final_answer  200ms
    ⎿  ready · 3.0k→400 tok
```

| 元素 | 含义 |
|------|------|
| `●` / `✗` / `◆` | 成功工具 / 错误 / 最终答案 |
| 主行 | 工具名 · 关键参数 · 耗时 |
| `⎿` | 体量+token，或错误首行 |
| 第三行（可选） | 错误时短 Thought；`-v` 可加 observation 预览 |

答案正文仍在 **stdout**；轨迹在 **stderr**。

### 何时不用 Live View

| 条件 | 回退 |
|------|------|
| stderr 非 TTY | 紧凑一行文本（Phase A compact） |
| `-v` / `--verbose` | 全量 Thought/Action/Observation + decide prompt |
| 未来可显式 `--plain` | （预留）强制纯文本 |

---

## 四、事件模型

```go
// 步中
type Progress struct {
    Phase  ProgressPhase // "decide" | "tool"
    Step   int
    Action string
    Args   map[string]any
}

// 步末（已有，略）
type Step struct { Index, Thought, Action, Args, Observation, … }
```

发射点（`internal/agent/react.go`）：

1. LLM `Chat` 前 → `ProgressDecide`  
2. `reg.Exec` 前 → `ProgressTool`  
3. 任意结局（tool / final / parse_error）→ `OnStep`

---

## 五、阶段路线

| Phase | 状态 | 内容 |
|-------|------|------|
| **A** 紧凑纯文本 | ✅ | 一行/步；失败展开；`-v` 全量 |
| **B** Live progressive UI | ✅ 默认 TTY | spinner + 卡片 + 颜色；对齐 CC 信息架构 |
| **B+** 可选全屏 TUI | ⬜ | Bubble Tea：可折叠历史、快捷键展开；`--tui` |
| **C** Web | ⬜ | SSE + 折叠卡片 + run 回放 |

说明：B 有意 **不做全屏接管**（与 Claude Code 的 inline progressive 更接近），避免和 `promptSave()`、管道、录屏打架。需要面板式交互时再上 B+。

---

## 六、实现地图

| 路径 | 职责 |
|------|------|
| `internal/agent` | `Progress` / `OnProgress` / `OnStep` |
| `internal/ui/agentview` | Live View：spinner、卡片、lipgloss 样式 |
| `internal/cmd/ask.go` | TTY 选 live；否则 / `-v` 用 `writeAgentStep` |
| `internal/cmd/ask.go` `writeAgentStep*` | 纯文本 compact / detailed |

---

## 七、验收

- [x] TTY 下 `ask --agent`：等待时有 spinner，完成时为卡片而非文本墙  
- [x] 错误步：红/✗ + `⎿` 错误首行，可选短 thought  
- [x] 成功步：不默认倾倒 Thought/Observation  
- [x] 非 TTY：无 spinner 控制序列动画；紧凑文本可用  
- [x] `-v`：仍可拿全量 transcript  
- [x] `OnProgress` 单测 + `agentview` 卡片单测  

---

## 八、非目标

- 不在 `agent` 包引入 UI 库  
- 不把完整 observation 默认打到 stderr  
- 本阶段不做可交互折叠快捷键（B+）  
- 不在本呈现层修复 FTS 查询语法（如中文分词/引号）；那是 search 工具问题  

---

## 九、参考

- [109-agent-exploration.md](109-agent-exploration.md)  
- Claude Code / 常见 coding agent：inline progressive disclosure  
- [Charm Lip Gloss](https://github.com/charmbracelet/lipgloss)  
- [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea) — B+ 候选  
