# 矛盾检测与处理策略

> 本文档描述 Wiki 健康检查中矛盾（contradiction）检测的设计思路、处理策略和使用指南。
> 最后更新：2026-07-06

---

## 一、问题背景

`ruminate lint` 会检测 Wiki 页面之间的事实性矛盾。当两个页面共享相同的 WikiLink 目标
（如同一实体、同一概念），但描述的事实相互冲突时，lint 会报告 warning 级别的矛盾。

然而，**同名不一定同义**。例如：

- `wiki/summaries/xxx.md` 描述"元宝"是中国古代流通货币
- `wiki/summaries/yyy.md` 描述"元宝"是张杰养的一只橘色流浪猫

这两个描述并不矛盾——只是同一名称在不同语境下指代不同实体（一词多义 / polysemy）。
如果 lint 每次都报告这类"假矛盾"，日积月累会让用户对 lint 报告产生疲劳，降低对
真实问题的敏感度。

## 二、矛盾分类

| 类型 | 说明 | 示例 | 处理方式 |
|------|------|------|----------|
| **真矛盾** | 同一实体在同一语境下事实冲突 | 页面A说元宝是唐朝货币，页面B说元宝是宋朝货币 | 人工审查并修改其中一个页面 |
| **假矛盾（一词多义）** | 同名但指代不同实体 | 页面A说元宝是货币，页面B说元宝是猫 | 告知用户，但不作为 warning 报告 |
| **视角差异** | 同一事实从不同角度描述，看似矛盾 | 页面A说项目很成功，页面B指出项目的问题 | 可能是 synthesis 页面的素材 |

## 三、解决方案：两层防御

### 第一层：LLM 提示词改进（源头减少误报）

在矛盾检测的 LLM 系统提示词中，明确要求模型区分 **POLYSEMY**（一词多义）和
**CONTRADICTION**（事实矛盾）：

- **POLYSEMY**：同名实体在不同语境/领域中指代完全不同的事物 → 报告为 `info` 级别，提示用户知晓但不作为问题
- **CONTRADICTION**：同一实体在同一语境下事实冲突 → 报告为 `warning` 级别，需要用户处理

这一层在 LLM 判断时就已经做了分流，能从源头过滤掉大部分假矛盾，**零额外成本**。

### 第二层：抑制机制（人工兜底）

即使 LLM 改进了，也难免有漏网之鱼。提供一个**矛盾抑制文件**
`.ruminate/lint-suppressions.json`，用户可以手动标记"这个不是真矛盾，以后跳过"。

#### 文件格式

```json
{
  "version": 1,
  "suppressions": [
    {
      "id": "a1b2c3d4",
      "check": "contradiction",
      "page": "wiki/summaries/xxx.md",
      "related_page": "wiki/summaries/yyy.md",
      "reason": "一词多义：元宝在xxx中指古代货币，在yyy中指宠物猫",
      "created_at": "2026-07-06T10:00:00Z"
    }
  ]
}
```

#### 匹配规则

抑制规则与 lint issue 匹配时，以下条件**全部满足**才算命中：

1. `check` 字段一致（如 `contradiction`）
2. 两条 `page` 路径与 issue 的 `Page` 和 `RelatedPage` **无序匹配**（A-B 和 B-A 都算匹配）

命中抑制规则的 issue 不会出现在 lint 报告中。

#### 管理方式

- **命令行**：`ruminate lint suppress --page <path> --related <path> --reason "..."` 添加抑制
- **手动编辑**：直接编辑 `.ruminate/lint-suppressions.json`（JSON 格式，简单直观）
- **查看**：`ruminate lint suppressions` 列出所有抑制规则

抑制文件位于 `.ruminate/` 目录下，会被 git 跟踪，团队成员可以共享抑制规则。

## 四、使用流程

### 场景：发现矛盾

```
$ ruminate lint

── Potential Contradictions ──

  ⚠ Contradiction between xxx and yyy
    Page: wiki/summaries/xxx.md
    Related: wiki/summaries/yyy.md
    The entity "元宝" is described as currency in xxx but as a cat in yyy.
```

### 步骤 1：判断矛盾类型

阅读两个页面的内容，判断：
- 是**同一事物的矛盾描述**？（真矛盾 → 步骤 2a）
- 是**同名但不同事物**？（一词多义 → 步骤 2b）

### 步骤 2a：修复真矛盾

编辑其中一个（或两个）页面的 Markdown 内容，使描述一致。然后重新运行 lint 确认矛盾已消除。

### 步骤 2b：抑制假矛盾

```bash
ruminate lint suppress \
  --page wiki/summaries/xxx.md \
  --related wiki/summaries/yyy.md \
  --reason "一词多义：元宝在xxx中指古代货币，在yyy中指宠物猫"
```

下次 lint 运行时，这个矛盾将不再出现。

### 步骤 3：定期回顾

建议定期查看抑制列表（`ruminate lint suppressions`），确认之前的判断仍然有效。
随着 Wiki 内容的增长，某些"一词多义"可能后来真的变成了矛盾（例如，后来发现
"元宝"这个猫的名字确实来源于古代货币的文化意象，两个页面可能需要交叉引用）。

## 五、设计决策

### 为什么不在 LLM 调用时直接传入已知的一词多义列表？

- 会增加 prompt 长度，且随抑制列表增长而线性增长
- 抑制列表可能很大（日积月累），不适合每次都塞进 prompt
- 后过滤（post-filter）模式更简单可靠，不依赖 LLM 的理解能力

### 为什么抑制文件是 JSON 而不是 Markdown frontmatter？

- 抑制是跨页面的关系（page A ↔ page B），不适合放在单个页面的 frontmatter 中
- JSON 结构化，程序处理可靠
- 集中在 `.ruminate/` 下，不污染 Wiki 内容文件

### 为什么不提供 `lint --fix` 自动修复矛盾？

- 矛盾的本质是**事实判断**，需要人的领域知识来裁决哪个版本是对的
- LLM 可以辅助分析，但不应代替人做事实裁决
- 如果将来要支持 `--fix`，也应限于机械性修复（如更新交叉引用），而非修改事实描述

## 六、未来扩展

- **抑制规则的过期机制**：可选的 TTL，过期后抑制规则自动失效，提醒用户重新审视
- **矛盾优先级**：根据页面重要性和矛盾程度排序，帮助用户聚焦关键问题
- **相似实体合并建议**：当多个页面描述同一事物但用了不同名称时，建议合并
