# 并发安全 — 模式 · 生命周期 · 竞态

**关切**：极端负载和取消场景下是否 crash、泄漏、竞态或假并发。评委看的不是「会不会写 goroutine」，而是退出路径、WaitGroup 是否真并行、channel 所有权是否清晰。

**适用场景**：goroutine/channel 设计、并发 CR、性能优化。  
**权威来源**：[Go Blog — Pipelines and cancellation](https://go.dev/blog/pipelines)、[Effective Go — Concurrency](https://go.dev/doc/effective_go#concurrency)、Google style decisions on goroutines。

与 [robustness](../robustness/README.md) 的分工：本模块管并发**结构与模式**；recover、锁数据逃逸等防御性边界见 robustness。

---

## 1. 基础原则

1. **Don't communicate by sharing memory; share memory by communicating**
2. **Main 负责生命周期**：谁启动 goroutine，谁确保其退出
3. **Context 传播取消**：`context.Context` 是首选取消机制
4. **Channel 所有权**：发送方 close；接收方不 close
5. **避免泄漏**：每个 goroutine 必须有退出路径

## 2. 核心模式

### 2.1 Worker Pool

固定数量 worker 消费 job channel：

```go
func worker(ctx context.Context, jobs <-chan Job, results chan<- Result) {
    for {
        select {
        case <-ctx.Done():
            return
        case job, ok := <-jobs:
            if !ok {
                return
            }
            results <- process(job)
        }
    }
}
```

**现代替代**：`golang.org/x/sync/errgroup` + `SetLimit`：

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(10)
for _, task := range tasks {
    g.Go(func() error {
        return process(ctx, task)
    })
}
if err := g.Wait(); err != nil {
    return err
}
```

### 2.2 Pipeline（流水线）

Stage 通过 channel 串联；每 stage 是 goroutine：

```go
func gen(ctx context.Context, nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for _, n := range nums {
            select {
            case out <- n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

func sq(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            select {
            case out <- n * n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}
```

**Stage 契约**：
- Stage 创建并 owns 其 output channel
- Stage goroutine 退出时 `defer close(out)`
- 每个 blocking send 应 `select` on `ctx.Done()`

### 2.3 Fan-Out / Fan-In

**Fan-out**：多 goroutine 从同一 input channel 读取，并行处理。

**Fan-in**：多 input channel merge 到单 output：

```go
func merge(ctx context.Context, cs ...<-chan int) <-chan int {
    var wg sync.WaitGroup
    out := make(chan int)

    output := func(c <-chan int) {
        defer wg.Done()
        for n := range c {
            select {
            case out <- n:
            case <-ctx.Done():
                return
            }
        }
    }

    wg.Add(len(cs))
    for _, c := range cs {
        go output(c)
    }

    go func() {
        wg.Wait()
        close(out)
    }()
    return out
}
```

注意：fan-out **不保证顺序**。

### 2.4 Or-Done / Cancellation

早期退出时 upstream 可能阻塞在 send。解决方案：

```go
// 方式 1: context（推荐）
select {
case out <- v:
case <-ctx.Done():
    return
}

// 方式 2: done channel（legacy pipeline 模式）
select {
case out <- v:
case <-done:
    return
}
```

`errgroup.WithContext`：任一 goroutine 返回 error → 自动 cancel 兄弟 goroutine。

### 2.5 Semaphore（限流）

```go
sem := make(chan struct{}, maxConcurrent)
sem <- struct{}{}        // acquire
defer func() { <-sem }() // release
```

或用 `errgroup.SetLimit(n)`。

### 2.6 sync 原语选用

| 需求 | 工具 |
|------|------|
| 互斥访问 | `sync.Mutex` / `sync.RWMutex` |
| 单次初始化 | `sync.Once` |
| 等待一组 goroutine | `sync.WaitGroup` |
| 并发安全 map | `sync.Map`（读多写少）或 mutex + map |
| 原子计数 | `sync/atomic` |

**Mutex vs Channel**：保护共享状态用 mutex；传递所有权/任务用 channel。

## 3. 错误传播

Pipeline 中 error 需显式设计：

```go
type Result struct {
    Value int
    Err   error
}

func stage(ctx context.Context, in <-chan Input) <-chan Result {
    out := make(chan Result)
    go func() {
        defer close(out)
        for v := range in {
            r, err := process(v)
            select {
            case out <- Result{r, err}:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}
```

Consumer 遇到第一个 error 时 cancel context，避免 upstream 阻塞。

## 4. 常见陷阱

| 陷阱 | 后果 | 预防 |
|------|------|------|
| 未 close channel | goroutine 永久阻塞 | defer close；用 range 消费 |
| 向 closed channel 发送 | panic | 确保单一 sender |
| 循环变量 + goroutine | 错误捕获（Go <1.22） | `go func(v T) { ... }(v)` |
| 无界 goroutine | OOM | worker pool / errgroup.SetLimit |
| 共享 mutable state | data race | mutex 或 channel |
| blocking send 无 cancel | 泄漏 | select + ctx.Done() |
| 非 Test goroutine 调 t.Fatal | crash | t.Error + return |
| net/http 式 recover panic | 掩盖 bug | 让 panic 冒泡到 monitoring |

## 5. 设计检查清单

- [ ] 每个 goroutine 有明确退出条件
- [ ] Context 传递且 blocking ops 尊重 cancellation
- [ ] Channel 发送方负责 close
- [ ] 并发度有界（pool / semaphore / SetLimit）
- [ ] Error 有传播路径（errgroup 或 Result type）
- [ ] 无 data race（`go test -race`）
- [ ] WaitGroup Add/Done 配对

## 6. 参考

- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go Blog: Context](https://go.dev/blog/context)
- [golang.org/x/sync/errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Memory Model](https://go.dev/ref/mem)
