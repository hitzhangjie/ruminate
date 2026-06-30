# 可测试性 — 设计 · 写法 · 充分性

**关切**：变更有没有安全网。评委区分「有测试」和「测了该测的」——设计是否逼出不可测代码、error path 和边界有没有覆盖，比测试文件数量更重要。

**适用场景**：编写测试、设计可测 API、CR 中的测试质量审查。
**权威来源**：Google [Decisions — Test structure](https://google.github.io/styleguide/go/decisions#test-structure)、[Useful test failures](https://google.github.io/styleguide/go/decisions#useful-test-failures)、[Best Practices — Tests](https://google.github.io/styleguide/go/best-practices#tests)， supplemented by Go community testability patterns.

Google 风格文档对测试**写法**着墨较多，对**可测试性设计**着墨较少。本章补齐设计侧指引。
单测命名、体量上限、接口测试要求等见 [practices §6](../practices/README.md#6-测试)。

---

## 1. 可测试性设计原则

**Good testability makes production code simpler**, not harder.

| 原则       | 做法                                                   |
| ---------- | ------------------------------------------------------ |
| 显式依赖   | Constructor injection，不在内部 `new` DB/HTTP client |
| 小接口     | 消费者定义 1-3 方法 interface，易写 fake               |
| 稳定边界   | 业务逻辑与 I/O 分离；domain 包无 network import        |
| 无隐藏状态 | 避免 package-level mutable state；必须有时可 reset     |
| 确定性     | 时间/随机/ID 通过 injectable 接口或参数传入            |
| 可观测     | 返回值优于 side-effect-only；error 有语义              |

### 1.1 依赖注入（Constructor Injection）

```go
type UserStore interface {
    GetUser(ctx context.Context, id int) (User, error)
}

type UserService struct {
    store UserStore
}

func NewUserService(store UserStore) *UserService {
    return &UserService{store: store}
}
```

- 小项目：手动 constructor injection 足够
- 大项目：可考虑 [google/wire](https://github.com/google/wire) 编译期 DI
- **Accept interfaces, return structs**

### 1.2 接口设计 for testability

```go
// Good: 小接口，消费者定义
type CreditCard interface {
    Charge(card *Card, amount Money) error
}

// Bad: 庞大 interface 仅为 mock
type PaymentProcessor interface {
    Charge(...) error
    Refund(...) error
    Subscribe(...) error
    // ... 20 methods
}
```

### 1.3 Test Doubles

| 类型           | 用途                             |
| -------------- | -------------------------------- |
| **Stub** | 固定返回值，无交互验证           |
| **Fake** | 可工作的简化实现（内存 DB）      |
| **Spy**  | 记录调用供断言                   |
| **Mock** | 预设期望 + 验证交互（gomock 等） |

**偏好顺序**：Stub/Fake > Spy > generated Mock。Hand-written fake 通常比 gomock 更易读。

命名（Google 风格）：

```go
package creditcardtest

type Stub struct{}
func (Stub) Charge(*creditcard.Card, money.Money) error { return nil }

type AlwaysDeclines struct{}
func (AlwaysDeclines) Charge(*creditcard.Card, money.Money) error {
    return creditcard.ErrDeclined
}
```

### 1.4 包布局

```
mypackage/           # 生产代码
mypackage_test/      # black-box 测试（仅 exported API）
creditcardtest/      # 共享 test doubles（testonly）
```

## 2. 测试结构与写法

### 2.1 核心规则：Leave testing to the Test function

- **Test helper**：setup/cleanup，调用 `t.Helper()`，失败用 `t.Fatal`
- **Assertion helper**：**不推荐** — 不用自建 assert 库
- 验证逻辑 inline 在 `Test` 函数，或用返回 `error` 的 validation 函数

```go
// Good: validation 返回 error，Test 决定是否 fail
func polygonCmp() cmp.Option { /* ... */ }

func TestFencepost(t *testing.T) {
    got := Fencepost(tomsDiner, 1*meter)
    if diff := cmp.Diff(want, got, polygonCmp()); diff != "" {
        t.Errorf("Fencepost(...) unexpected diff (-want +got):\n%s", diff)
    }
}
```

### 2.2 测试组织：子测试与表驱动

Go 里两种常用的组织方式，**不互斥**——选哪种取决于 case 之间是「同一逻辑的不同入参出参」，还是「本质不同的场景」。

**怎么选**：先问「这些 case 是不是在测同一件事、只是参数不同？」——是则用表驱动；否则用子测试分开写。

#### 子测试：按场景拆分

每个 subtest 是一个**独立场景**——setup、调用路径或断言逻辑可以不同，不必强行统一成一张表。

```go
func TestUserService(t *testing.T) {
    t.Run("creates_user", func(t *testing.T) {
        svc := NewUserService(fakeStore{user: User{ID: 1}})
        got, err := svc.Create(ctx, CreateRequest{Name: "alice"})
        if err != nil {
            t.Fatalf("Create(...) error = %v", err)
        }
        if got.ID != 1 {
            t.Errorf("Create(...) ID = %d, want 1", got.ID)
        }
    })

    t.Run("returns_error_when_store_fails", func(t *testing.T) {
        svc := NewUserService(failingStore{})
        _, err := svc.Create(ctx, CreateRequest{Name: "alice"})
        if !errors.Is(err, ErrUnavailable) {
            t.Errorf("Create(...) error = %v, want ErrUnavailable", err)
        }
    })
}
```

#### 表驱动：同一场景的不同入参出参

同一函数、同一断言结构，仅 input/output 变化——用 slice of struct 集中维护 case，避免复制粘贴。

```go
func TestParseIP(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    []string
        wantErr bool
    }{
        {name: "ipv4", input: "1.2.3.4", want: []string{"1", "2", "3", "4"}},
        {name: "bad_host", input: "hostname", wantErr: true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseIP(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseIP(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
                return
            }
            if !cmp.Equal(got, tt.want) {
                t.Errorf("ParseIP(%q) = %v, want %v", tt.input, got, tt.want)
            }
        })
    }
}
```

**通用要点**（两种组织方式均适用）：

- subtest 名称简短、无空格/斜杠（影响 `go test -run` 过滤）；长描述放 struct 的 `desc` 字段，打印在 failure message 中
- 并行：`t.Parallel()` 在 test 和 subtest 中；Go 1.22+ 循环变量安全
- 表驱动 case 若需 per-case setup 或断言分支过多，应拆成独立子测试而非继续加字段

### 2.3 有用失败信息 (Useful Test Failures)

格式：`FuncName(%v) = %v, want %v`（**got before want**）

- 含函数名、输入、实际值、期望值
- 复杂结构：`cmp.Diff(want, got)` + `(-want +got)` 标注
- 优先 `t.Error` 多次报告；仅后续无意义时用 `t.Fatal`
- 不用 `reflect.DeepEqual`；用 `google/go-cmp`
- **不** string-match 外部依赖的 error message（change-detector test）
- 测 error 语义：`errors.Is` / `errors.As` / `cmpopts.EquateErrors`

### 2.4 Test Helpers

```go
func mustLoadDataset(t *testing.T) []byte {
    t.Helper()
    data, err := os.ReadFile("testdata/dataset")
    if err != nil {
        t.Fatalf("Setup: could not load dataset: %v", err)
    }
    return data
}
```

- Setup 失败 → `t.Fatal`（在 helper 中 OK）
- 用 `t.Cleanup` 注册 teardown
- **禁止**在非 Test goroutine 中调用 `t.Fatal`/`t.FailNow`

### 2.5 Setup 作用域

- 每个 test 按需 setup，不用 `init()` 加载全部 fixture
- 昂贵共享 setup：`sync.Once` 懒加载（无 teardown 时）
- 全包共享 + teardown：自定义 `TestMain`（最后手段）

### 2.6 集成测试

- 优先 **real transport**（真实 HTTP/gRPC client 连 test server）
- 用作者提供的 test library（如 `fstest.TestFS`、`spannertest`）
- Acceptance test：独立 `packagetest` 包，返回 `error` 而非 `t.Fatal`

## 3. 测试类型策略

| 类型         | 范围                    | 速度 | 何时用                     |
| ------------ | ----------------------- | ---- | -------------------------- |
| Unit         | 单函数/方法             | 快   | 逻辑、边界、error path     |
| Table-driven | 同一场景多 input/output | 快   | 规则型逻辑、解析/校验      |
| Integration  | 组件 + real deps        | 中   | 协议/序列化/DB             |
| Acceptance   | 实现者合约验证          | 中   | 库的使用者需实现 interface |
| Fuzz         | 随机输入                | 慢   | 解析器、编解码             |

### 3.1 风险驱动测试

- 测 behavior，非 implementation detail
- 避免 change-detector test（绑定私有实现）
- 优先测 public API（black-box `package_test`）

### 3.2 充分性标准（评委视角）

「有测试」≠「测够了」。优先覆盖**高风险路径**：

| 必测                                  | 可不测或薄覆盖       |
| ------------------------------------- | -------------------- |
| 所有 error return 路径                | 纯 getter / 一行转发 |
| 边界值、非法输入                      | 第三方 SDK 薄封装    |
| 并发 / 竞态相关逻辑（配合 `-race`） | 生成代码             |
| 状态机转换、幂等、重试分支            |                      |

「单测不够充分」通常指：happy path 有测，但 error path、并发极端情况、关键分支未覆盖。

## 4. 可测试性反模式

| 反模式                  | 问题           | 修复                  |
| ----------------------- | -------------- | --------------------- |
| 内部创建 DB/HTTP client | 无法隔离       | Constructor injection |
| 全局变量                | 测试间污染     | 注入或 reset hook     |
| `time.Now()` 硬编码   | 非确定性       | 注入 clock            |
| 巨大 interface          | fake 难写      | 拆小 interface        |
| assert 库               | 失败信息丢失   | inline + cmp          |
| init() 加载全部 fixture | 单测慢         | 按需加载              |
| goroutine 中 t.Fatal    | 数据竞争/crash | t.Error + return      |

## 5. 检查清单

### 设计

- [ ] 依赖通过 constructor 注入
- [ ] I/O 与 domain logic 分离
- [ ] 时间/随机/ID 可注入
- [ ] 无 untestable global state

### 测试

- [ ] 组织方式选对：同场景用表驱动，异场景用子测试；case 多时用 `t.Run` 包裹
- [ ] 失败信息含 func name + input + got/want
- [ ] 用 cmp，不用 reflect.DeepEqual
- [ ] error 断言用 errors.Is/As
- [ ] helpers 调 t.Helper()
- [ ] 无 assert 库

## 6. 参考

- [Go Wiki: TableDrivenTests](https://go.dev/wiki/TableDrivenTests)
- [google/go-cmp](https://pkg.go.dev/github.com/google/go-cmp/cmp)
- [testing/fstest](https://pkg.go.dev/testing/fstest)
- Google TotT: Effective Testing, Risk-driven Testing, Change-detector Tests Considered Harmful
