# 可观测性 — 日志 · 监控 · 排障

**关切**：线上出问题时，能否在合理时间内定位根因。评委看的是「失败了有没有留下足够线索」，不是「日志打得够不够多」。

**适用场景**：服务端、网络 I/O、异步任务、错误处理路径。  
与 [error-handling](../error-handling/README.md) 的分工：error-handling 管语义与传播；本模块管**记录与上报**。

---

## 1. 日志级别

| 级别 | 用途 |
|------|------|
| Error | 需要人工介入或告警的异常；每条应 actionable |
| Warn | 降级、重试、可自愈的异常 |
| Info | 关键业务里程碑（启动、配置加载、状态切换） |
| Debug / V(n) | 排障细节；用 `log.V(n)` 或 level check 避免昂贵序列化 |

- 不用 Error 记录预期内的业务拒绝（如参数校验失败 → 通常 Info/Debug + 返回 error）
- 同一类事件日志风格一致（字段顺序、换行、中英文择一）

## 2. 日志内容

每条错误日志应能回答：**什么操作、什么输入、什么结果、底层原因**。

```go
log.Errorf("save user id=%s: %v", id, err)
// 更好：结构化字段
slog.ErrorContext(ctx, "save user failed", "user_id", id, "err", err)
```

- panic recover 时记录 **stack trace**（`debug.Stack()`）
- 避免只打 `err.Error()` 而无业务上下文

## 3. Return vs Log

- 默认 **return error 给 caller 决定**是否 log；避免同一错误 return 又 log（重复、级别混乱）
- 例外：goroutine / 回调中错误无法 return 时，必须 log；网络 I/O 即使不关心业务结果，**传输层 err 也应记录**

```go
go func() {
    if err := conn.Write(msg); err != nil {
        log.Warnf("notify downstream: %v", err) // 无法 return，必须留痕
    }
}()
```

## 4. 监控与上报

- 关键路径：延迟、错误率、饱和度（队列深度、连接数）
- panic、OOM、goroutine 泄漏应有告警
- 指标命名稳定、带 service/module 维度，便于聚合

## 5. 上下文传递

- 优先 `context.Context` 携带 trace id / request id，日志与 span 关联
- 跨 goroutine 传递时复制必要字段，不共享可变 log buffer

## 6. 检查清单

- [ ] 错误日志含足够定位上下文（id、操作名、关键参数）
- [ ] 日志级别与语义匹配
- [ ] 异步 / 网络路径的错误有留痕
- [ ] panic 有 stack
- [ ] 无 return+log 重复（或有注释说明）
- [ ] 关键 SLI 有监控覆盖

## 7. 参考

- [error-handling](../error-handling/README.md)
- [robustness](../robustness/README.md)
