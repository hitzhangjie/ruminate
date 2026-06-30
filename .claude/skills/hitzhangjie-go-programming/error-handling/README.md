# 错误处理 — 语义 · 传播 · 边界

**关切**：失败是否可见、语义是否一致、能否被上层正确处理。评委反感吞 err、混用 error/错误码/bool、无装饰传播——本质都是「故障被藏起来了」。

**适用场景**：error 设计/传播/检查、panic 边界。日志与上报见 [observability](../observability/README.md)。  
**权威来源**：[Decisions — Errors](https://google.github.io/styleguide/go/decisions#errors)、[Best Practices — Error handling](https://google.github.io/styleguide/go/best-practices#error-handling)。  
panic / recover 的硬性边界见 [practices §4](../practices/README.md#4-错误处理)。

---

## 1. 基本约定

### 1.1 返回 error

```go
func Do() error                          // 仅 error
func Lookup(key string) (string, error)  // value + error，error 在最后
func Exists(key string) (string, bool)   // 简单存在性用 bool
```

- `nil` error = 成功；error != nil 时其他返回值**不可依赖**（除非文档说明）
- 接受 `context.Context` 的函数通常应返回 `error`（含 cancellation）
- 导出函数返回 `error` 接口，不返回 `*os.PathError` 等具体类型

### 1.2 Error string 格式

- 小写开头，无结尾标点（error 会被嵌入更大消息）
- 日志/测试/UI 中的完整消息可大写

```go
// Good
return fmt.Errorf("launch codes unavailable: %v", err)

// Bad — 重复底层信息
return fmt.Errorf("could not open settings.txt: %v", err)
// → "could not open settings.txt: open settings.txt: no such file..."
```

### 1.3 处理 error 的三种选择

1. **Handle** — 就地处理
2. **Return** — 向上传播（加 context）
3. **Terminal** — `log.Fatal` / `panic`（极少）

**禁止**静默丢弃 `_ = err`；若确实安全，加注释说明。

### 1.4 Indent error flow

```go
// Good
u, err := db.UserByID(id)
if err != nil {
    return fmt.Errorf("load user: %w", err)
}
// use u

// Bad
if err != nil {
    // handle
} else {
    // normal path indented
}
```

## 2. 结构化 Error

### 2.1 Sentinel errors

```go
var (
    ErrNotFound   = errors.New("not found")
    ErrPermission = errors.New("permission denied")
)

// 检查
if errors.Is(err, ErrNotFound) { /* ... */ }
```

- 包级 exported sentinel，名称 `ErrXxx`
- 用 `errors.Is` 检查（支持 wrapped error）
- **禁止** string matching / regexp on `err.Error()`

### 2.2 Custom error types

```go
type NotFoundError struct {
    Resource string
    ID       string
}

func (e *NotFoundError) Error() string {
    return fmt.Sprintf("%s %q not found", e.Resource, e.ID)
}

// 检查
var nfe *NotFoundError
if errors.As(err, &nfe) { /* use nfe.ID */ }
```

带 programmatic 信息的 error 应结构化（如 `os.PathError` 的 `.Path`）。

### 2.3 Error wrapping — %v vs %w

| Verb | 用途 | 效果 |
|------|------|------|
| `%v` | 添加人类可读 context；或转换/隐藏底层 error | 不可 unwrap |
| `%w` | 保留底层 error 供 `errors.Is`/`As` | 可 unwrap |

```go
// %w — 内部 helper，caller 需检查底层
return fmt.Errorf("couldn't find remote file: %w", err)

// %v — 系统边界（RPC/HTTP），翻译为 canonical error
return status.Errorf(codes.Internal, "couldn't find database: %v", err)
```

**%w 位置**：一般放末尾 `[context]: %w`；sentinel 分类 error 可放开头以便阅读：

```go
return fmt.Errorf("%w: invalid header: %v", ErrParse, detail)
```

**禁止**无信息增量的 wrap：

```go
return fmt.Errorf("failed: %v", err) // 直接 return err
```

## 3. 错误处理模式

### 3.1 逐层添加上下文

```go
func (s *Server) handleRequest(...) error {
    if err := s.repo.Save(ctx, obj); err != nil {
        return fmt.Errorf("save object %q: %w", obj.ID, err)
    }
    return nil
}
```

每层添加**该层独有**的信息，不重复底层已有内容。

### 3.2 错误分类处理

```go
switch {
case errors.Is(err, ErrNotFound):
    return nil, status.Error(codes.NotFound, err.Error())
case errors.Is(err, ErrPermission):
    return nil, status.Error(codes.PermissionDenied, err.Error())
default:
    return nil, fmt.Errorf("internal: %w", err)
}
```

### 3.3 批量操作 — errgroup

```go
g, ctx := errgroup.WithContext(ctx)
for _, item := range items {
    item := item
    g.Go(func() error {
        return process(ctx, item)
    })
}
if err := g.Wait(); err != nil {
    return err // 第一个 error，其余 cancel
}
```

仅第一个 error 有用时，errgroup 是合理例外（可忽略个别 error）。

### 3.4 Must 函数

```go
func MustParse(s string) time.Time {
    t, err := time.Parse(time.RFC3339, s)
    if err != nil {
        panic(err)
    }
    return t
}
```

- 仅用于**程序启动**或**测试 setup**（`mustXxx(t *testing.T)`）
- 正常运行路径用 error return

## 4. Panic 策略

| 场景 | 做法 |
|------|------|
| 正常 error | 返回 error |
| 程序配置错误（main） | `log.Exit` + 可操作消息 |
| API misuse（库） | panic（如 reflect、out of range） |
| 不可恢复 invariant 违反 | `log.Fatal`（不用 panic，defer 可能死锁） |
| Parser 内部 | panic + defer recover → error（**不跨越包边界**） |

**禁止**：
- 用 panic 做正常流程控制
- 在 HTTP handler 中 recover 掩盖 bug（net/http 的历史例外不应复制）
- 随意 recover 传播 corrupted state

## 5. Logging 与 Error

详细日志级别、上下文、return vs log 策略见 [observability](../observability/README.md)。本节只保留与 error 语义直接相关的原则：

- **Return OR log, not both**（除非有明确理由）；让 caller 决定
- 若选择 log，级别与 error 语义一致（见 observability）

## 6. 测试中的 Error

```go
// Good: 语义检查
if !errors.Is(err, ErrNotFound) {
    t.Errorf("Get(%q) error = %v, want ErrNotFound", id, err)
}

// Good: 仅关心有无 error
if (err != nil) != tt.wantErr {
    t.Errorf("f(%q) error presence = %v, want %v", tt.input, err != nil, tt.wantErr)
}

// Bad: string match 外部 error
if err.Error() != "some message from dependency" { ... }
```

## 7. 检查清单

- [ ] error 是最后一个返回值
- [ ] early return，无 else 嵌套
- [ ] 不丢弃 error（或注释说明）
- [ ] wrap 用 `%w` 且位置正确
- [ ] sentinel 用 `errors.Is`/`As`，非 string match
- [ ] 系统边界翻译为 canonical error（gRPC status 等）
- [ ] 不用 panic 做正常流程
- [ ] 测试不断言 error string（外部依赖）

## 8. 参考

- [Effective Go — Errors](https://go.dev/doc/effective_go#errors)
- [Go Blog — Error handling](https://go.dev/blog/error-handling-and-go)
- [Go Blog — Go 1.13 errors](https://go.dev/blog/go1.13-errors)
- [pkg/errors](https://pkg.go.dev/errors) — Is, As, Unwrap
