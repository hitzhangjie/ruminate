# 设计与可维护性 — API · 结构 · 演进

**关切**：后人能否读懂、安全改动、在不破坏调用方的前提下演进。评委看包结构、函数粒度、接口边界、导出范围——都是在问「六个月后的自己能不能改」。

**适用场景**：API 设计、架构评审、可读性 mentor 级别的 CR。  
**权威来源**：[Style Decisions](https://google.github.io/styleguide/go/decisions) + [Best Practices](https://google.github.io/styleguide/go/best-practices)。

在 [style-guide](../style-guide/README.md) 基础之上，强调**读者视角**的清晰度与长期可维护性。  
函数/文件行数、嵌套深度、参数个数等量化标准见 [practices](../practices/README.md)。

---

## 1. 可读性原则

优先级：**Clarity > Simplicity > Concision > Maintainability > Consistency**

### 1.1 回答两个问题

1. **What** — 代码在做什么？（命名、结构、分解）
2. **Why** — 为什么这样做？（注释解释非显而易见的 rationale）

注释解释 **why**，不重复 **what**。代码应自解释；注释用于语言细节、业务 nuance、性能权衡。

### 1.2 简单性

- 从上到下可读，不假设读者记住前文
- 无不必要抽象层
- 复杂度 deliberate 添加，且附带文档和测试
- API 简单 vs 实现简单：权衡时优先让**调用方**正确使用

## 2. 命名与 API 设计

### 2.1 避免重复（Best Practices）

```go
// Bad                          // Good
package yamlconfig              package yamlconfig
func ParseYAMLConfig(...)       func Parse(...)
func (c *Config) WriteConfigTo  func (c *Config) WriteTo
func OverrideFirstWithSecond     func Override
```

- 返回值是名词：`JobName(key) (value, ok bool)`
- 副作用是动词：`WriteDetail(w io.Writer)`
- 仅类型不同时后缀类型名：`ParseInt` / `ParseInt64`

### 2.2 变量命名

- 名称反映**当前用途**，而非来源（protobuf 字段名 ≠ 最佳局部变量名）
- 省略类型词：`userCount` 非 `numUsers`；`users` 非 `userSlice`
- 作用域大 → 名长；作用域小 → 可单字母（`i`, `r`, `w`）

### 2.3 包设计

- 单一职责；避免 "utility" 包
- 紧密耦合的内部类型放同一包，用 unexported 隐藏
- 测试 double 包：`creditcardtest`，类型名 `Stub`（非 `StubCreditCardService`）
- 包大小：过大则考虑拆分；过小且仅被一处使用则考虑合并

### 2.4 接口

- **消费者定义接口**（Accept interfaces, return structs）
- 接口小而 focused；1-2 方法的 interface 最常见
- 不为 mock 而 mock — 只在需要 substitutability 时抽象
- 见 [Best Practices — Interfaces](https://google.github.io/styleguide/go/best-practices#interfaces)

## 3. 代码组织

### 3.1 函数

- 一个函数一个清晰职责
- 参数过多 → 考虑 Options struct 或分解
- 参数顺序：context 第一，error 最后返回
- 体量参考：函数 ≤ 80 行（推荐）、嵌套 ≤ 4 层（必须）、参数 ≤ 5 个（推荐）→ [practices §3.5](../practices/README.md#35-必须推荐体量与嵌套)

### 3.2 控制流

- **Line of sight**：减少 nesting，early return
- 复杂条件拆为命名变量：

```go
leap4 := year%4 == 0
leap100 := year%100 == 0
leap400 := year%400 == 0
leap := leap4 && (!leap100 || leap400)
```

- 关键单行差异加注释（`= ` vs `:=`，中间 `!`）

### 3.3 全局状态

- 最小化 package-level 变量
- 需要时用 `sync.Once` 或显式 init，文档说明线程安全
- 测试间不泄漏全局状态

## 4. 文档与注释

### 4.1 导出符号

- 每个 exported 标识符有 doc comment，以名称开头
- 提供 **Runnable Example**（放 `*_test.go`，出现在 godoc）
- 说明 concurrency safety、是否 nil-safe、error 语义

### 4.2 注释类型

| 类型 | 用途 |
|------|------|
| Doc comment | 导出 API 契约 |
| 行内注释 | 解释 why（非常规逻辑、性能 hack） |
| TODO | 带 issue 编号 |
| Deprecated | 说明替代方案 |

### 4.3 错误文档

- 文档说明哪些 error 可被 `errors.Is`/`errors.As` 检查
- 说明非 error 返回值在 error != nil 时是否有效

## 5. 可维护性模式

### 5.1 可预测命名

- 同一概念在不同函数间参数名一致（便于 refactor）
- `MustXYZ` 仅用于启动阶段；测试中 `mustXYZ(t)` 用于 setup

### 5.2 依赖最小化

- 少依赖 = 少 breakage
- 不依赖 undocumented 行为

### 5.3 演进友好

- 用 struct 配置而非长参数列表，便于加字段
- 用 embedded struct 做版本化 API extension

## 6. Review 检查清单

### 结构
- [ ] 函数职责单一，长度合理（见 [practices 速查表](../practices/README.md#速查表)）
- [ ] 控制流 flat，early return
- [ ] 无 hidden coupling / 全局 mutable state

### 命名与 API
- [ ] 命名无冗余，符合 MixedCaps
- [ ] 导出 API 有 doc comment
- [ ] 接口由消费者定义，足够小

### 注释
- [ ] 注释解释 why，非 what
- [ ] 非常规/性能关键代码有说明

### 一致性
- [ ] 与同包/同项目风格一致
- [ ] 不 worsening 已有 style deviation

## 7. 参考

- [Style Guide — Style principles](https://google.github.io/styleguide/go/guide#style-principles)
- [Decisions — Commentary](https://google.github.io/styleguide/go/decisions#commentary)
- [Best Practices — Documentation, Package size, Global state](https://google.github.io/styleguide/go/best-practices)
