# 工程实践 — 可量化、可扫描的落地标准

**定位**：与关切维度子模块（理念层）并列的**实践层**。理念回答「为什么在意」；本章回答「多少算过关、扫描工具怎么判」。

**适用场景**：配置 linter、CR 中需要明确判定时、晋升/作品级评审的硬性检查。  
**来源**：在 [Google Go Style Guide](https://google.github.io/styleguide/go/guide) 基础上，结合国内大型团队落地经验沉淀；与 Google canonical **不矛盾时**作为团队补充。

> 部分指标看似生硬，往往是因为很多人连这条底线都做不到，也不理解背后的理念。理解「为什么」之后，应追求更简洁，而不是卡在阈值边缘。

---

## 要求等级

| 等级 | 含义 | CR / 扫描 |
|------|------|-----------|
| **必须（Mandatory）** | 必须采用；扫描工具视为错误 | Must fix |
| **推荐（Preferable）** | 理应采用；特殊情况可破格，需在 CR 说明 | Should fix |
| **可选（Optional）** | 自行决定 | Consider |

破格时：说明原因 + 是否有后续拆分/重构计划。

---

## 速查表

| 指标 | 阈值 | 等级 | 关切模块 |
|------|------|------|----------|
| 格式化 | `gofmt` / `goimports` 通过 | 必须 | [style-guide](../style-guide/README.md) |
| 行宽 | ≤ 120 列 | 推荐 | style-guide |
| 单文件行数 | ≤ 800 行 | 必须 | readability |
| 单函数行数 | ≤ 80 行（含注释、空行） | 推荐 | readability |
| 嵌套深度 | ≤ 4 层 | 必须 | readability |
| 函数参数个数 | ≤ 5 个 | 推荐 | readability |
| 圈复杂度 | ≤ 10（linter 配置） | 推荐 | readability |
| 单测文件行数 | ≤ 1600 行（2× 普通文件） | 必须 | testability |
| 单测函数行数 | ≤ 160 行（2× 普通函数） | 必须 | testability |
| 接收器命名 | 函数 > 20 行时不用单字符 | 推荐 | style-guide |
| `switch` | 必须有 `default` | 必须 | style-guide |
| `goto` | 业务代码禁止 | 必须 | style-guide |
| 循环内 `defer` | 禁止 | 必须 | style-guide |
| 导出符号 | 必须有 doc comment | 必须 | readability |
| 应用服务 | 必须有接口测试 | 必须 | testability |

函数行数计量：从签名左括号下一行起，到右括号上一行止。

---

## 1. 代码风格

### 1.1 【必须】格式化

- 所有代码经 `gofmt` 格式化；import 经 `goimports` 管理。
- 运算符与操作数之间留空格；作下标或实参时紧凑（`a[i]`、`f(x)`）。

### 1.2 【推荐】换行与行宽

- 建议一行不超过 **120 列**；过长时优先重构，而非机械折行。
- **例外**（可超列数，勿为满足列数而折行）：
  - 函数签名（过长往往意味着参数过多，应重构）
  - 长字符串字面量、原始字符串、struct tag
  - import 路径、工具生成代码、文档 URL 注释

```go
// 长签名可超列数
func (i *webImpl) GenerateAgentInstallLink(ctx context.Context, req *pb.GenerateAgentInstallLinkRequest) (*pb.GenerateAgentInstallLinkResponse, error) {
    ...
}

// 不要仅为满足列数在签名处折行——gofmt 对齐会降低可读性
```

与 [style-guide](../style-guide/README.md) 一致：**禁止**在缩进变化处折行；**禁止**为缩短长字符串而拼接折行。

### 1.3 【必须】import

- 标准库 / 本地包 / 第三方包分组，组间空行；不用相对路径 import。
- 包名与路径不一致或多包名冲突时用别名；无冲突且包名合规时不用别名。
- 匿名 import 单独分组并注释用途（`embed` 除外）。

---

## 2. 命名与文件

### 2.1 【必须】文件命名

- 小写 + 下划线分词：`user_service.go`（文件名与标识符 MixedCaps 规则独立）。

### 2.2 【推荐】包命名

- 与目录一致；短小有意义；避免 `util`、`common`、`misc`、`global`。
- 允许收敛型子包，如 `xx/util/encryption`。

### 2.3 【必须】标识符

- 驼峰；导出与否由首字母大小写决定。
- 专有名词：`apiClient`（私有且为首词）、`APIClient`、`UserID`；不用 `UrlArray`。
- 生成代码（如 `*.pb.go`）可豁免。

### 2.4 【推荐】接口命名

- 单方法接口以 `er` 结尾（`Reader`）；两方法综合命名；三方法以上类似结构体名。

---

## 3. 控制结构与函数

### 3.1 【推荐】if / for / return

- `if` 用初始化语句建立局部变量；变量在左、常量在右（`err != nil`，非 `nil != err`）。
- bool 直接判断，不写 `== true` / `== false`。
- 有错误立即 return，正常路径不缩进在 `else` 中。

### 3.2 【必须】range / switch / goto

- 只需 key 时丢弃 value；只需 value 时用 `_`。
- `switch` **必须有 `default`**。
- 业务代码**禁止 `goto`**。

### 3.3 【必须】defer

- 资源释放紧跟成功路径之后 defer；**先确认无 error 再 defer Close**。
- **禁止在循环中 defer**；用立即执行函数包裹：

```go
for _, v := range values {
    func() {
        fields, err := db.Query(v)
        if err != nil { ... }
        defer fields.Close()
        ...
    }()
}
```

### 3.4 【推荐】函数参数与接收器

- 参数 ≤ **5** 个；过多时考虑 Options struct 或分解。
- `map` / `slice` / `chan` / `interface` 传值不传指针。
- 接收器：类型首字母缩写；**禁止** `me` / `this` / `self`。
- 函数超过 **20** 行时接收器不用单字符。

### 3.5 【必须/推荐】体量与嵌套

| 项 | 阈值 | 等级 |
|----|------|------|
| 文件行数 | 800 | 必须 |
| 函数行数 | 80 | 推荐 |
| 嵌套深度 | 4 | 必须 |
| 圈复杂度 | 10 | 推荐 |

嵌套过深时提取子函数（early return、拆 `HasArea` 类 helper）。理念见 [readability §3](../readability/README.md)。

### 3.6 【必须】魔法数字

- 重复出现或含义不直观的字面量用命名常量/变量替代。
- **不必**为上下文已一目了然的数建常量（如 `for i := 0`、`x%2 == 0`、`os.Exit(1)`）。
- 错误消息格式串等若与使用处相距很远，强行提成常量反而降低可读性。

---

## 4. 错误处理

与 [error-handling](../error-handling/README.md)、[robustness](../robustness/README.md) 配合使用。

### 4.1 【必须】error

- 返回的 `error` 必须处理或显式忽略（`_ = err` 需注释说明）。
- 多返回值时 `error` 在**最后**；错误描述**无句尾标点**。
- 独立错误流；`err` 不与其它条件混判：

```go
// Bad
if err != nil || y == nil { return err }

// Good
if err != nil { return err }
if y == nil { return errors.New("some error") }
```

### 4.2 【必须】panic

- 一般错误用 `error`，不用 `panic`。
- 可对**不变量**断言；`init` / 全局初始化失败可 panic（`MustCompile` 等）。
- 导出方法一般不 panic；必须 panic 的用 `MustXxx` 命名并文档说明。
- 不建议用 `log.Fatal` 作断言。

### 4.3 【必须】recover

- 仅在 `defer` 中使用；业务逻辑一般不 recover。
- 只捕获**明确类型**的 panic，禁止吞掉所有异常后继续跑 corrupted state。

---

## 5. 注释

与 [readability §4](../readability/README.md) 配合。导出符号均须有 doc comment。

| 对象 | 格式 |
|------|------|
| 包 | `// Package 包名 描述`（main 除外，多文件包一处即可） |
| 结构体/接口 | `// 类型名 描述` |
| 方法/函数 | `// 函数名 描述` |
| 变量/常量/类型别名 | `// 名称 描述` |

- 结构体导出字段：生僻或歧义字段须注释。
- 例外无需注释：`Write`/`Read`、`ServeHTTP`、`String`、`Error`/`Unwrap`、`Len`/`Less`/`Swap`。
- 注释掉的代码提交前删除，除非注明保留原因与后续计划。

---

## 6. 测试

与 [testability](../testability/README.md) 配合。

### 6.1 【必须】结构与命名

- 测试文件：`*_test.go`；函数以 `Test` 开头。
- `func Foo` → `Test_Foo`；`func (b *Bar) Foo` → `TestBar_Foo`（下划线仅用于此场景）。

### 6.2 【必须】体量

- 单测文件 ≤ **1600** 行；单测函数 ≤ **160** 行。
- 圈复杂度、列数、import 分组与普通文件一致。

### 6.3 【必须】覆盖要求

- 每个重要的可导出函数应有测试，与实现同 PR 提交。
- **应用服务必须有接口测试**。

---

## 7. 依赖管理

### 7.1 【必须】Go modules

- Go 1.11+ 使用 modules；`go.sum` 须提交，勿加入 `.gitignore`。

### 7.2 【推荐】vendor 与 module 路径

- 使用 modules 的项目建议不提交 `vendor`。
- 内部工程 module 名建议与仓库路径一致，便于引用。

---

## 8. 与 Google canonical 的差异

| 点 | Google / 理念层 | 实践层 | 处理 |
|----|-----------------|--------|------|
| 行宽 | 无固定列数 | 120 列推荐 | 实践层为团队补充，不 override 折行原则 |
| 文件名 | 未强调 | snake_case 必须 | 实践层补充 |
| blank import | 库包尽量避免 | 允许 + 注释 | 以 style-guide 为准，实践层为可选放宽 |

冲突时：**Google canonical > 关切模块理念 > 本章实践阈值**。

---

## 9. 工具落地

| 工具 | 用途 |
|------|------|
| `gofmt` | 格式唯一真相 |
| `goimports` | import 增删与分组 |
| `go vet` | 静态分析（编译前） |
| `golangci-lint` 等 | 行宽、圈复杂度、函数长度、嵌套深度等阈值 |

配置 linter 时以本章「必须」项为 error、「推荐」项为 warning 为宜。

---

## 10. 按关切索引

| 关切 | 实践章节 |
|------|----------|
| 代码规范 | §1、§2、§3.1–3.2 |
| 设计与可维护性 | §3.4–3.6、§5 |
| 错误处理 / 防御性 | §4 |
| 可测试性 | §6 |
| 依赖与交付 | §7 |

理念与惯用法细节仍查各关切子模块；需要数字或扫描判定时查本章。
