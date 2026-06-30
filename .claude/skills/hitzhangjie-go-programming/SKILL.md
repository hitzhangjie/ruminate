---
name: go-programming
description: >-
  Go 工程能力成长地图：从基础规范到设计、可测试性、健壮性、可观测性的
  递进关切体系。帮助开发者觉察短板、知其然更知其所以然，并体系化沉淀
  判断力。Use when learning Go engineering practices, self-assessing code
  quality, preparing for promotion review, or coaching engineers to grow.
slug: hitzhangjie-go-programming
version: 1.0.0
displayName: Go 编程进阶指引
---

# Go 工程能力成长 Skill

## 这是什么

Go 工程实践里「一山更有一山高」：能跑起来只是起点；往后还有代码规范与硬性底线、错误处理与并发安全、可读性与设计、质量内建与可测试性、防御性编程与健壮性，再到真实上线后的可观测与快速定位。很多人并非完全不会，而是：

- **有短板** — 某些关切维度长期欠账，自己没意识到
- **未领悟** — 只停留在「能写出来」，没理解背后的工程代价
- **知其形不知其意** — 照抄模式（接口、表驱动、defer），说不清为什么、何时不该用
- **知其意未体系化** — 零散经验多，遇到新场景仍靠直觉，无法举一反三

本 skill 把分散的规则、实践与判断标准**按关切维度组织**，让人看清自己在哪一层、缺哪一块、下一步该补什么。这也是晋升时代码委员会的责任：不只判对错，更要帮人建立**可迁移的判断力**，是一份实打实的、硬核能力地图。

## 成长阶梯

关切维度大致对应工程师常见的成长路径——不是严格的职级对应，而是**意识递进**：

| 阶段 | 典型心态 | 主要关切 | 子模块 |
|------|----------|----------|--------|
| **傻大黑粗能跑就行** | 功能优先，风格随缘 | 协作成本、失败可见、底线安全 | [style-guide](style-guide/README.md)、[error-handling](error-handling/README.md)、[concurrency](concurrency/README.md) |
| **可读性可维护性及规范** | 开始考虑后人能否维护 | 结构、命名、API 演进 | [readability](readability/README.md) |
| **可测试性及质量内建** | 变更要有安全网 | 可测设计、测试策略 | [testability](testability/README.md) |
| **健壮性及防御性编程** | 异常输入、边界、资源不能漏 | 防御性编程、panic/锁/资源 | [robustness](robustness/README.md) |
| **可观测性及问题定位** | 出问题要能快速找到根因 | 日志、指标、追踪 | [observability](observability/README.md) |

**硬性落地标准**（多少算过长、Must/Prefer/Optional、工具怎么扫）集中在 [practices](practices/README.md)。理念层回答「为什么在意」；实践层回答「多少算过关」——两者配合，避免只记阈值不理解原因，或只讲理念无法自检。

## 自我觉察：怎么用本 skill

### 1. 定位自己在哪

读代码或复盘自己的 PR 时，按关切维度逐项自问（只加载相关子模块）：

| 关切 | 自问 |
|------|------|
| **代码规范** | diff 是否聚焦逻辑？风格是否与团队一致？ |
| **可维护性** | 六个月后的自己能否安全改动？API 能否演进？ |
| **可测试性** | 这次变更有无安全网？设计是否逼出不可测代码？ |
| **错误处理** | 失败是否可见、可传播、语义一致？ |
| **并发安全** | 极端情况下会否 crash、泄漏、竞态？ |
| **防御性编程** | 异常输入、panic、锁、资源边界是否守住？ |
| **可观测性** | 线上出问题能否快速定位？ |

### 2. 区分「知道」与「掌握」

| 状态 | 表现 | 下一步 |
|------|------|--------|
| **不知道** | 从没想过这个维度 | 读对应子模块的理念章节 |
| **知其形** | 能套用模式，说不清取舍 | 读子模块中的 rationale、反例 |
| **知其意** | 能解释为什么，但凭感觉 | 对照 [practices](practices/README.md) 量化自检 |
| **体系化** | 新场景能主动选对关切、权衡取舍 | 辅导他人、反哺子模块案例 |

### 3. 按场景深入学习

**写新功能**
1. [style-guide](style-guide/README.md) — 基础规范
2. [practices](practices/README.md) — 需要明确体量/格式阈值时
3. 有 I/O / goroutine → [error-handling](error-handling/README.md) + 视情况 [concurrency](concurrency/README.md) / [robustness](robustness/README.md)
4. 服务端 / 长生命周期进程 → [observability](observability/README.md)
5. 与实现同步 → [testability](testability/README.md)
6. API / 包结构 → [readability](readability/README.md)

**复盘自己的代码**
1. 按成长阶梯逐项过关切维度（只加载触及的模块）
2. [practices](practices/README.md) — 硬性 Must/Prefer 自检
3. 记录：哪个维度是短板？是「不知」还是「未体系化」？

**补测试**
1. [testability](testability/README.md) — 主模块
2. [style-guide](style-guide/README.md) — 测试命名与包布局
3. [error-handling](error-handling/README.md) — error 断言语义

## 理念与实践

| 层 | 模块 | 回答什么 |
|----|------|----------|
| **理念** | 各关切子模块 | 为什么在意、原则与惯用法（Google canonical） |
| **实践** | [practices](practices/README.md) | 多少算过关、Must/Prefer/Optional、工具怎么扫 |

需要判定「函数多长算过长」「嵌套几层算深」时查 **practices**；需要理解设计取舍时读对应关切模块。自检或辅导时：**先按关切归类问题，再落到具体细则**——分类本身就是在练判断力。

## 关切维度与子模块

| 关切 | 核心问题 | 子模块 |
|------|----------|--------|
| **代码规范** | 团队协作成本：diff 能否聚焦逻辑、风格是否一致 | [style-guide](style-guide/README.md) |
| **设计与可维护性** | 后人能否读懂、改动是否安全、API 能否演进 | [readability](readability/README.md) |
| **可测试性** | 变更有没有安全网、设计是否逼出不可测代码 | [testability](testability/README.md) |
| **错误处理** | 失败是否可见、可传播、语义是否一致 | [error-handling](error-handling/README.md) |
| **并发安全** | 极端情况下是否 crash、泄漏、竞态 | [concurrency](concurrency/README.md) |
| **防御性编程** | 异常输入、panic、锁、资源边界是否守住 | [robustness](robustness/README.md) |
| **可观测性** | 线上出问题能否快速定位 | [observability](observability/README.md) |

## 权威来源层级

| Layer | Source | Binding |
|-------|--------|---------|
| Style Guide | [guide](https://google.github.io/styleguide/go/guide) | **Canonical** |
| Style Decisions | [decisions](https://google.github.io/styleguide/go/decisions) | Normative |
| Best Practices | [best-practices](https://google.github.io/styleguide/go/best-practices) | Advisory |

冲突时 **guide > decisions > best-practices > [practices](practices/README.md)**。关切子模块承载理念；practices 承载可量化落地标准，不与之矛盾。

## 成长反馈结构

用于自我复盘、同伴互评，或代码委员会辅导时，**先按关切维度组织，再落到具体细则**——帮对方看见「问题属于哪类关切」，而不只是「这里写错了」。

### 整体评级（作品级复盘）

| 评级 | 含义 |
|------|------|
| **A** | 各关切维度整体达标，仅少量可优化点 |
| **B** | 有写好代码的意愿和能力，但 1–2 个关切维度有明显短板 |
| **C** | 多个关切维度同时有明显欠账 |

### 输出结构

```markdown
## 评级：B

### 做得好的地方
（按关切维度肯定，每条对应一个维度——帮人看见自己已经掌握什么）

### 待提升的关切
#### 代码规范
- [细则] ……（引用 style-guide 中的具体条目）
#### 设计与可维护性
- ……
（仅列出有问题的维度；每个维度下 1–3 条具体发现，附最小改法示例）

### 成长建议
（1–2 句：本次最该优先补的关切维度及原因——指向「下一步学什么」，不重复罗列细则）
```

日常 PR 互评可省略评级，保留「按关切维度归类 + 具体细则 + 改法示例」即可。

严重级别（互评 / 委员会场景）：
- **Must fix** — 健壮性 / 并发安全 / 错误处理硬伤，或违反 canonical style / practices 中「必须」项
- **Should fix** — 可维护性、可测试性、可观测性明显欠账，或 practices「推荐」项未达标且无说明
- **Consider** — 规范细节、最佳实践建议、practices「可选」项
