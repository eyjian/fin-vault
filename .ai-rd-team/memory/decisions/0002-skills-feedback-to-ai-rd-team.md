---
number: 0002
title: Skills 资产归属与反哺 ai-rd-team 的时机
status: Accepted
date: 2026-05-16
deciders: [主理人, AI 架构顾问]
related:
  - .ai-rd-team/skills/README.md
  - .ai-rd-team/skills/naming-conventions.md
  - .ai-rd-team/skills/decimal-precision.md
  - .ai-rd-team/skills/llm-provider-pattern.md
  - .ai-rd-team/skills/error-handling.md
  - .ai-rd-team/skills/repository-pattern.md
  - .ai-rd-team/skills/go-gin-development.md
  - /data/workspace/github/eyjian/ai-rd-team/README.md
tags: [skills, ai-rd-team, governance, feedback-loop]
---

# 0002 · Skills 资产归属与反哺 ai-rd-team 的时机

## 背景

fin-vault 在 Phase 0 Batch A 产出了 6 份 skill：
`naming-conventions` / `decimal-precision` / `llm-provider-pattern` / `error-handling` / `repository-pattern` / `go-gin-development`，
当前位置在 `<workspace>/.ai-rd-team/skills/`。

ai-rd-team 框架本身约定了 skill 的三层结构（见 `ai-rd-team/openspec/specs/design/05-roles-skills.md` §6.1）：

```
内置 Skills（ai-rd-team 仓库 builtin/）   → 跨项目通用基础能力
   ↓
全局 Skills（~/.ai-rd-team/skills/）      → 个人跨项目偏好
   ↓
项目 Skills（<workspace>/.ai-rd-team/）   → 单项目规范
```

需要回答：**这 6 份 skill 应该住在哪一层？是否、何时、以何种形式反哺到 ai-rd-team builtin？**

ADR 0001 已确定技术选型；本 ADR 只决定 skill **资产归属与反哺策略**，不涉及内容本身。

## 候选方案

### 方案 A：当前直接提交到 ai-rd-team builtin

把 6 份 skill 原样推到 `ai-rd-team/ai_rd_team/skills/builtin/`。

- ✅ 立即可被任何用 ai-rd-team 的项目复用
- ❌ 内容深度绑定 fin-vault：表名 `t_fv_*`、字段 `f_*`、`internal/llm/` 包路径、错误码区间 10000-99999、DECIMAL(20,2)/(20,8) 选型、`domain.Asset/Holding/Transaction` 等具体实体、五层目录结构
- ❌ 强加 fin-vault 的业务取舍给其他项目（其他项目可能用 `int64×100` 表示金额、可能用 Kratos 五层、可能不用模块前缀）
- ❌ 违反 ai-rd-team 三层 skill 设计意图（builtin 应中性）

### 方案 B：保留在 fin-vault 项目级，未来抽通用层反哺 builtin（**采纳**）

当前位置不动；在三个条件全部满足时，再做一次性"抽通用 + 留特化"的拆分反哺。

- ✅ 符合 ai-rd-team 三层设计：fin-vault 的具体决策属于"项目级覆盖"
- ✅ 与 ADR 0001 整体节奏一致（YAGNI / 推迟引入）
- ✅ 与之前 Q1=B「仓库 + 反哺」的策略选择延续
- ⚠️ 暂时无法被其他项目直接复用（但 ai-rd-team builtin 当前也未实现）

### 方案 C：现在就拆成"通用版 + fin-vault 覆盖"双份

每份 skill 拆成中性 builtin 版 + fin-vault workspace 覆盖版，两边同步维护。

- ✅ 立即让 ai-rd-team 有 builtin
- ❌ 工作量翻倍，且容易抽得不彻底（残留项目味道）
- ❌ ai-rd-team 自身 builtin 框架尚未实际建立（`ai_rd_team/skills/builtin/` 目录还没启动）
- ❌ 单一项目验证下抽出的"通用层"很可能不通用，反哺后还要再改

## 决策

采纳 **方案 B**：

1. 6 份 skill **保留在 `<fin-vault>/.ai-rd-team/skills/`**，作为 fin-vault 项目级资产，**当前不向 ai-rd-team 仓库提交**。
2. 反哺动作（拆"通用 builtin"+"fin-vault workspace 覆盖"）**推迟**到三个门槛条件全部满足时启动。
3. 反哺时按预设映射表执行（见下文「反哺映射表」），保证一次性拆清楚。

### 反哺触发条件（三条件 AND）

| # | 条件 | 验证方式 |
|---|------|---------|
| C1 | ai-rd-team 仓库的 `ai_rd_team/skills/builtin/` 目录已实际建立、有至少 1 份真实 builtin skill | 在 ai-rd-team 仓库 `find ai_rd_team/skills/builtin -name "*.md"` 非空 |
| C2 | fin-vault 这 6 份 skill 已支撑至少 2 个里程碑落地（M1 核心模块 + M2 报表/AI 模块），内容稳定 1 个月以上无大改 | `git log --since="30 days ago" .ai-rd-team/skills/` 提交频率低于每周 1 次 |
| C3 | 至少有第二个项目（fin-vault 之外）开始用 ai-rd-team，能验证"哪些是真通用、哪些是 fin-vault 特殊" | 新项目仓库出现 `.ai-rd-team/skills/` 且至少引用过 1 份 builtin skill |

### 反哺映射表

| 当前 skill（fin-vault） | 反哺后 builtin（ai-rd-team） | fin-vault workspace 覆盖（保留） |
|---|---|---|
| `naming-conventions.md` | `db-naming-conventions.md`（中性的"前缀机制 + 模块前缀模板"，**不**指定具体前缀） | 仅保留 `t_fv_` 表前缀、`f_` 字段前缀、7 个固定模块（user/dict/core/quote/ai/report/sys） |
| `decimal-precision.md` | `decimal-precision.md`（介绍 `shopspring/decimal` 通用用法、精度设计原则） | 仅保留"DECIMAL(20,2) 用于法币、DECIMAL(20,8) 用于持仓数量、DECIMAL(20,4) 用于汇率"的 fin-vault 取舍 |
| `repository-pattern.md` | `repository-pattern.md`（中性的 Repository + UnitOfWork 模式说明） | 仅保留 fin-vault 的具体 entity 清单（Asset/Holding/Transaction/...）和 `t_fv_core_*` 表名 |
| `error-handling.md` | `error-handling.md`（中性的 errs 包模式、错误码区间机制） | 仅保留 `pkg/errs` 路径、10000-99999 区间映射、Response 结构体字段 |
| `llm-provider-pattern.md` | `llm-provider-pattern.md`（**几乎可全部进 builtin**：`LLMProvider` 接口 + Registry + OpenAI 兼容协议 + 5 个厂商 BaseURL） | 仅保留 `internal/llm/` 包路径、fin-vault 的 Provider 装配代码示例 |
| `go-gin-development.md` | **改名** `go-gin-clean-arch.md`（中性的 Gin + Clean Architecture 五层指引），与规划中的 `go-kratos-development.md` 平级 | 仅保留 fin-vault 的 `internal/{domain,repository,service,handler,llm,bootstrap}/` 具体路径与 UnitOfWork 落地 |

> 反哺执行时新建 ADR 0003（`Superseded by` 关系不必要，本 ADR 是"治理决策"而非"内容决策"，0003 只描述执行结果）。

## 后果

✅ **正面**
- 6 份 skill 内容可以**充分携带 fin-vault 决策细节**，对项目内成员/Agent 有最强约束力（不被中性 builtin 稀释）
- 节奏与 ADR 0001 一致：先在自己项目跑通，再考虑通用化
- 避免"用单一项目当样本抽通用层"的常见错误（抽出来的"通用"往往只是该项目的同义改写）

⚠️ **代价**
- 短期内其他项目无法直接复用这 6 份 skill 的成果
- 反哺时需要一次性投入（估算 1-2 天）做拆分与改名
- 需要持续关注 ai-rd-team 仓库 builtin 进展，否则 C1 条件可能长期不满足

🔁 **重新评估触发条件**（任一满足即重写本 ADR）
- ai-rd-team 主理方明确征集 builtin 贡献且对内容耦合度容忍度高 → 提前部分反哺
- 出现第三方项目直接 fork 这 6 份 skill 改用 → 说明通用价值已被验证，可提前拆分
- 6 个月内 C1/C2/C3 任意一条无法满足 → 重新评估"是否还要反哺"或"是否合并到全局 skills（`~/.ai-rd-team/skills/`）作为个人偏好层"

## 升级路径

```
T0（当前）：6 份 skill 全部在 fin-vault workspace 层
    │
    ├── M1 完成 → 验证内容稳定性（C2 部分满足）
    │
    ├── ai-rd-team builtin 启动（C1 满足）
    │
    ├── 第二个项目接入 ai-rd-team（C3 满足）
    │
    ▼
T1（反哺执行，新建 ADR 0003）：
    ai-rd-team/ai_rd_team/skills/builtin/         （通用层）
    └── 6 份按映射表抽出的中性 skill
    fin-vault/.ai-rd-team/skills/                 （覆盖层）
    └── 6 份精简后的项目特化 skill（仅留 fin-vault 决策）
```

业务代码、Agent 调用方式、Skill 加载机制**均无变化**，仅是内容拆分与文件归属调整。
