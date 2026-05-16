# decisions/ —— ADR 架构决策记录

本目录存放**架构决策记录（Architecture Decision Records, ADR）**，按编号顺序追加。

## 何时写 ADR

满足任一即必须写：

1. 引入或推迟一个**核心依赖库**（影响业务代码的库，如 ORM、AI 客户端、缓存）
2. 选择一种**架构模式**（如三层资产模型、事件溯源、Repository 模式）
3. 在多个**等价方案**中做出选择（影响后续维护）
4. **推迟**某个能力到下一阶段（写明触发条件）
5. **撤销/修改**之前的 ADR（新建 ADR 引用旧编号，标记旧为 `Superseded`）

## ADR 文件命名

`NNNN-kebab-case-title.md`，编号从 0001 起递增，**不复用、不删除**。

## ADR 模板

```markdown
---
number: 000X
title: 简短决策标题
status: Proposed | Accepted | Deprecated | Superseded by 000Y
date: YYYY-MM-DD
deciders: [架构师 / 主理人]
related:
  - REQUIREMENT.md
  - docs/architecture-design.md
tags: [tech-stack, db, ai]
---

# 000X · 决策标题

## 背景（Context）
为什么需要做这个决策？外部约束是什么？

## 候选方案（Options）
- 方案 A：...
- 方案 B：...
- 方案 C：...

## 决策（Decision）
我们选择**方案 X**，因为 ...

## 后果（Consequences）
- ✅ 正面影响
- ⚠️ 潜在代价
- 🔁 触发重新评估的条件

## 升级路径
未来何时、如何切换到其他方案。
```

## 当前 ADR 列表

| # | 标题 | 状态 |
|---|---|---|
| 0001 | 第一阶段技术栈选型与"暂不引入"清单 | Accepted |
| 0002 | Skills 资产归属与反哺 ai-rd-team 的时机 | Accepted |
