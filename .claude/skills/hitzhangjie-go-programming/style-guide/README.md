# 代码规范 — 编码风格 · 惯用法 · 常用写法

**关切**：团队协作成本。风格一致让 reviewer 把精力放在逻辑与设计上，而不是在 diff 里纠格式、命名、imports。细则本身不需要再解释「为什么」——它们服务的就是这个关切。

**适用场景**：编写 Go 代码、日常 CR 中的风格检查。  
**权威来源**：[Go Style Guide (guide)](https://google.github.io/styleguide/go/guide) — canonical + normative。  
**量化门槛**（行宽、defer、switch 等）见 [practices](../practices/README.md)。

---

## 1. 格式化

- 所有 `.go` 文件必须通过 `gofmt`（生成代码用 `format.Source`）
- 无固定行宽；行过长时优先重构，而非机械折行
- **禁止**在缩进变化处（函数声明、条件分支）折行
- **禁止**为缩短 URL 等长字符串而折行

## 2. 命名 — MixedCaps

- 多词标识符用 `MixedCaps` / `mixedCaps`，不用 snake_case
- 常量同样遵循 MixedCaps（不用 `MAX_SIZE`、`kMaxSize`）
- 局部变量视为 unexported 命名

### 2.1 包名

- 全小写、无下划线、尽量短（`tabwriter` 而非 `tab_writer`）
- 避免 `util`、`common`、`helper`、`model` 等无信息包名
- 避免与常见局部变量冲突（`usercount` 优于 `count`）
- Black-box 测试包：`linkedlist_test`（不是 `linked_list_test`）

### 2.2 缩写与首字母缩略词

| 英文 | Exported | Unexported |
|------|----------|------------|
| URL | URL | url |
| ID | ID | id |
| HTTP | HTTP | http |
| iOS | IOS | iOS |
| gRPC | GRPC | gRPC |

### 2.3 其他命名规则

- **Receiver**：短、一致、类型缩写（`func (s *Server)`），不用 `this`/`self`
- **Getter**：不用 `Get` 前缀（`Counts()` 而非 `GetCounts()`）；远程/昂贵操作用 `Fetch`/`Compute`
- **变量长度**：与作用域成正比、与使用频率成反比
- **避免重复**：`user.Count()` 而非 `user.GetUserCount()`；包名已提供上下文
- **下划线**：标识符中一般不用，例外：测试函数名、仅生成代码导入的包、cgo/syscall

## 3. 核心惯用法 (Idioms)

### 3.1 错误检查 — 标准形式

```go
if err := doSomething(); err != nil {
    return err
}
```

与常见写法**不同**时，用注释强调（如 `err == nil` 表示成功路径）。

### 3.2 多返回值

- 最后一个返回值是 `error`
- 不用 in-band 错误（`-1`、`nil` 表示失败）；用 `(value, ok bool)` 或 `(value, error)`
- 导出函数返回 `error` 接口，不返回具体错误类型（避免 nil 指针被包装成非 nil error）

### 3.3 Nil slice

```go
var s []T        // Good: nil slice
s := []T{}       // Bad: 不必要的 empty slice
```

- API 不区分 nil slice 与 empty slice；用 `len(s) == 0` 判断空
- 局部变量优先 `var s []T` 初始化

### 3.4 结构体字面量

- **跨包类型必须写字段名**
- 多行字面量：闭括号单独一行，与开括号同级缩进
- 零值字段可省略（提高可读性）
- 表驱动测试中：复杂 case 用命名字段，成功 case 省略 error 相关零值字段

### 3.5 函数签名

- 函数/方法声明签名保持单行
- 参数列表过长时，与其折行不如重构

### 3.6 条件与循环

- 错误/终端条件先处理，正常路径不缩进在 `else` 中
- 变量使用超过几行时，不用 `if x, err := f(); err != nil` 形式，改为先声明再 `if`

### 3.7 Least mechanism（最少机制）

1. 优先语言内建（slice、map、channel、struct）
2. 其次标准库
3. 最后才引入新依赖

集合成员检查：`map[string]bool` 通常足够，不必引入 set 库。

### 3.8 常用标准库模式

```go
// strings.Cut — 清晰的分割
before, after, found := strings.Cut(s, sep)

// defer 释放资源
f, err := os.Open(name)
if err != nil {
    return err
}
defer f.Close()

// context 传递
func Do(ctx context.Context, ...) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
}
```

### 3.9 导入

- 标准库 / 第三方 / 本项目 三组，空行分隔
- 禁止 blank import（`import _ "pkg"`）在库包中；仅 main 或需要 side-effect 的包中，且需注释为何需要
- 重命名 importName 时跨文件保持一致（import importName "importPath"）

### 3.10 零值可用性

设计类型时让零值有意义（如 `sync.Mutex`、`bytes.Buffer`），减少初始化样板。

## 4. 语言特性选用

| 场景 | 推荐 |
|------|------|
| 简单集合 | slice / map |
| 并发通信 | channel + select |
| 互斥 | `sync.Mutex` / `sync.RWMutex` |
| 单次初始化 | `sync.Once` |
| 字符串拼接（少量） | `+` 或 `fmt.Sprintf` |
| 字符串拼接（循环） | `strings.Builder` |
| 配置/选项 | functional options 或 Config struct |

## 5. 编码检查清单

- [ ] `gofmt` / `go vet` 通过
- [ ] MixedCaps，无 snake_case 标识符
- [ ] 包名简短有意义
- [ ] 错误在最后返回值，early return
- [ ] 无 in-band error
- [ ] 跨包 struct literal 有字段名
- [ ] nil slice 语义一致
- [ ] 无 blank import（库包）
- [ ] 无 `Get` 前缀 getter（除非 HTTP GET 语义）

## 6. 参考

- [Go Style Guide — Core guidelines](https://google.github.io/styleguide/go/guide#core-guidelines)
- [Go Style Decisions — Naming, Language, Imports](https://google.github.io/styleguide/go/decisions)
