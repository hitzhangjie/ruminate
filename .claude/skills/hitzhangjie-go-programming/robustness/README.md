# 防御性编程 — 健壮性

**关切**：程序在异常输入、并发竞争、资源边界下能否守住 invariant，而不是 silent corruption 或进程级 crash。评委看的是「极端情况有没有想过」，不是「happy path 能不能跑」。

**适用场景**：服务端 PR 默认检查、长生命周期 goroutine、对外 API、持锁逻辑。  
**权威来源**：[error-handling](../error-handling/README.md)（错误语义）、[concurrency](../concurrency/README.md)（并发原语），本节聚焦防御性边界。

---

## 1. 入参与契约

- 导出函数 / 公开 RPC handler：校验 nil、空字符串、越界、非法枚举；非法输入返回明确 error，不 panic
- 文档说明哪些前置条件由 caller 保证、哪些由函数自身保证
- 集中式 validate（如 `func (r *Request) Validate() error`），避免大段内联校验散落各处

```go
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
    if id == "" {
        return nil, fmt.Errorf("get user: %w", ErrInvalidArgument)
    }
    // ...
}
```

## 2. Nil 与指针

- 解引用前检查；返回 slice/map 时说明 nil 与 empty 的语义
- 接口值为 nil 时注意 typed nil trap（`var e *MyErr; return e` 不是 nil error）

## 3. 类型断言与转换

- 断言必须检查 `ok`；若刻意忽略，注释说明为何安全

```go
v, ok := x.(string)
if !ok {
    return fmt.Errorf("expected string, got %T", x)
}
```

## 4. Goroutine 与 panic

- 长生命周期 / 无法由 caller 感知的 goroutine：顶层 `defer recover`，记录 stack 后退出或上报；**不**用 recover 吞掉后继续跑 corrupted state
- 不把 recover 当正常流程；HTTP handler 层面 recover 是框架职责，业务 goroutine 自行兜底
- 每个 goroutine 有明确退出路径（见 [concurrency](../concurrency/README.md)）

```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            log.Errorf("worker panic: %v\n%s", r, debug.Stack())
        }
    }()
    // ...
}()
```

## 5. 锁与共享数据

- 持锁期间不做 I/O、不 call 外部代码（避免死锁、锁持有时间过长）
- 返回受锁保护的数据：返回 **copy**，不把内部 slice/map 引用直接暴露给调用方
- RWMutex：写路径用 `Lock`，读多写少才用 `RLock`；注意读锁下不可升级写锁

```go
func (c *Cache) Get(key string) (Val, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.data[key]
    return v, ok // Val 是值类型；若存指针则返回拷贝
}
```

## 6. Context 生命周期

- `context.WithCancel` / `WithTimeout` 的 `cancel` 必须 `defer` 调用，防泄漏
- 不把 `context.Background()` 传入需要取消的长任务；子任务用父 ctx 派生
- 不在 struct 中长期存储 ctx（除 request-scoped handler）

## 7. 资源与连接

- `defer Close()`；网络 / DB 连接注意空闲超时、池大小、健康检查
- 临时性错误（`net.Error` 的 `Temporary()`/`Timeout()`、gRPC `codes.Unavailable`）：区分可重试与应退出；循环中不要遇错即永久退出

## 8. 全局状态

- 最小化 package-level 可变变量；必须有时文档说明并发安全
- get/set 全局配置注意 happens-before（见 [Go Memory Model](https://go.dev/ref/mem)）；测试间不泄漏状态

## 9. 检查清单

- [ ] 公开 API 校验入参
- [ ] 无 unchecked 类型断言
- [ ] 长生命周期 goroutine 有 recover + 退出路径
- [ ] 锁保护数据不外泄引用
- [ ] ctx cancel 已 defer
- [ ] 临时错误有重试 / 退避策略（非一刀切退出）
- [ ] `go test -race` 通过

## 10. 参考

- [concurrency](../concurrency/README.md)
- [error-handling](../error-handling/README.md)
- [observability](../observability/README.md)
