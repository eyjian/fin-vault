---
type: memory
layer: agent.d
author: manual
created: 2026-05-17T09:10:00+08:00
updated: 2026-05-17T09:10:00+08:00
related:
  - REQUIREMENT.md
  - .ai-rd-team/config.yaml
  - openspec/changes/replace-ai-with-trpc-agent-go/
tags: [onboarding, entrypoint, mandatory, codebuddy]
estimated_tokens: 720
---

# 团队启动入口（强制加载，最高优先级）

> 本文件是 **ai-rd-team 任何角色加入项目时的第一份强制阅读材料**，无论 IDE 是 ai-rd-team 原生壳还是 CodeBuddy 等外部 IDE。读完本文件并通过 §3 自检之前，**禁止开始任何编码、设计、评审动作**。

## 1. 必读顺序（不可乱序）

| 步骤 | 文件 | 角色 | 强度 |
|---|---|---|---|
| ① | [REQUIREMENT.md](../../../REQUIREMENT.md) | 全员 | 必读 |
| ② | `.ai-rd-team/memory/agent.d/`（本目录全部 5 份） | 全员 | 必读 |
| ③ | [openspec/changes/](../../../openspec/changes/) 下**当前活跃议题** | 全员 | 必读 |
| ④ | `.ai-rd-team/skills/`（按角色加载，见 [config.yaml](../../config.yaml)） | 按角色 | 必读 |
| ⑤ | 三件套深读：按你的角色定位（见 [REQUIREMENT.md](../../../REQUIREMENT.md) §7 文档地图） | 按角色 | 必读 |
| ⑥ | `.ai-rd-team/memory/decisions/`（ADR） | 全员 | 浏览 |
| ⑦ | `.ai-rd-team/runtime/data-task-breakdown.yaml` | 全员 | 浏览 |

> ⚠️ 步骤 ③ 的"当前活跃议题"由 `openspec list` 输出的非 archive 目录决定。截至 2026-05-17，唯一活跃议题为 `replace-ai-with-trpc-agent-go`（详见 §5）。

## 2. 角色定位与文档深读

| 角色 | 必须深读的源文档 |
|---|---|
| architect | `docs/architecture-design.md`、`docs/domain-model.md`、`docs/upgrade-guide.md` |
| developer | `docs/domain-model.md`、`docs/database-schema.md`、`docs/design/data-interfaces.yaml` |
| reviewer | `docs/delivery/checklist.md`、`.ai-rd-team/skills/error-handling.md`、`.ai-rd-team/skills/llm-provider-pattern.md` |
| tester | `docs/delivery/checklist.md`、`docs/design/data-interfaces.yaml` |
| pm / 主理人 | 全部（仅读，不直接产出代码） |

## 3. 启动自检（≤200 字输出，等待主理人确认后再开工）

完成 §1 与 §2 后，**必须**输出一条结构化自检消息，包含 5 项：

1. **角色与实例号**：如 "developer_1"
2. **已加载 skills 列表**：必须**全部以 `fin-vault-` 开头**；若出现 `python-best-practices` / `pytest-guide` 等 builtin 默认值，**立即停止并报告**（[REQUIREMENT.md](../../../REQUIREMENT.md) §9）
3. **当前活跃议题**：从 `openspec list` 取，确认与 §1 步骤 ③ 一致
4. **本次任务边界**：用一句话陈述"我接下来要做什么、不做什么"，必须不越过 [REQUIREMENT.md](../../../REQUIREMENT.md) §3 的"不做清单"
5. **未决问题**：如有，列出 1～3 条，留给主理人答复；无则写 "无"

## 4. 项目级铁律（违反任一即上升）

| # | 铁律 | 违规处理 |
|---|---|---|
| F1 | 金额禁用 `float32/float64`，统一 `decimal.Decimal` | reviewer 直接打回 |
| F2 | service 层禁止直接 import `gorm.io/gorm`、`go-redis`、`go-openai`、`trpc-agent-go`，仅依赖 `internal/repository` `internal/cache` `internal/llm` 抽象 | reviewer 直接打回 |
| F3 | 表名 `t_fv_{module}_{name}`、字段 `f_` 前缀（[REQUIREMENT.md](../../../REQUIREMENT.md) §6） | reviewer 直接打回 |
| F4 | 引入新依赖前**必须**先到 `.ai-rd-team/memory/decisions/` 写 ADR 论证（YAGNI） | architect 评审通过后方可 |
| F5 | 越界 [REQUIREMENT.md](../../../REQUIREMENT.md) §3 的"不做清单" | 立即停止并上升 main |
| F6 | 修改源文档（`REQUIREMENT.md`、`docs/*.md`、本目录文件） | 严禁，仅主理人可改 |
| F7 | 修改未归档的 OpenSpec 议题（`openspec/changes/<id>/`）需 architect + 主理人双签 | reviewer 拦截 |
| F8 | 与现有指令冲突时，**优先级**：本文件 > `agent.d/` 其他文件 > `skills/` > `decisions/` > `docs/` 三件套 | 自行判断，记入消息 |

## 5. 当前活跃议题指针（动态更新）

- **议题 ID**：`replace-ai-with-trpc-agent-go`
- **路径**：[openspec/changes/replace-ai-with-trpc-agent-go/](../../../openspec/changes/replace-ai-with-trpc-agent-go/)
- **核心交付物**：`proposal.md` / `design.md` / `tasks.md` / `specs/{ai-session,ai-agent-runtime,ai-tools}/spec.md`
- **状态查询**：`openspec status --change replace-ai-with-trpc-agent-go`
- **校验命令**：`openspec validate replace-ai-with-trpc-agent-go --strict`

> ⚠️ 与 [project-overview.md](./project-overview.md) "YAGNI 推迟 Eino/langchaingo" 条款的**关系澄清**：Eino 仍处推迟状态（v0.9.0-alpha 不稳定），本议题选用的是 **tRPC-Agent-Go v1.9.x**（已 GA），属于对 AI 框架决策的**新评估**，不破坏 YAGNI 原则。详见议题 `design.md` D1。

## 6. CodeBuddy IDE 等外部 IDE 切换说明

切到任何**非 ai-rd-team 原生壳**的 IDE（如 CodeBuddy）时：

1. 把本文件路径作为 IDE 启动后给 AI 的**第一条指令**：
   > "强制阅读 `.ai-rd-team/memory/agent.d/onboarding-entrypoint.md`，按其 §1～§3 执行，自检通过前禁止编码。"
2. IDE 自身的全局指令（如 CodeBuddy 内置规范）**不得覆盖**本目录任一文件；若冲突按 §4 F8 处理。
3. IDE 没有 `send_message` 等 ai-rd-team 内部协作工具时，沟通走 git commit message + PR description；任务拆分仍以 `.ai-rd-team/runtime/data-task-breakdown.yaml` 为唯一来源。

## 7. 升级与维护

- 本文件由主理人维护，团队成员**只读**（违反即触发 §4 F6）。
- 当 §5"当前活跃议题"变化时，由主理人或 architect 同步更新本节并 commit。
- 增加新铁律时追加到 §4，不替换已有条目。
