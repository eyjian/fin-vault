---
number: 0001
title: 第一阶段技术栈选型与"暂不引入"清单
status: Accepted
date: 2026-05-16
deciders: [主理人, AI 架构顾问]
related:
  - REQUIREMENT.md
  - docs/architecture-design.md
  - .ai-rd-team/memory/agent.d/tech-stack-selected.md
tags: [tech-stack, dependencies, yagni]
---

# 0001 · 第一阶段技术栈选型与"暂不引入"清单

## 背景

fin-vault 的核心约束是「**本地起步、升级改动最小**」。第一阶段必须满足：单机本地运行、SQLite 存储、零外部依赖、可在不修改业务代码的前提下切换到分布式部署。同时项目核心是 AI 能力（买卖建议、盈亏分析、报表生成），需要支持 DeepSeek / GLM / Kimi / 通义千问 / Ollama 等多模型切换。

技术栈最初草案曾考虑 langchaingo、Eino 自建薄 Agent 层、Snowflake、`golang-migrate`、Wire、`gopdf` 等，经红队复盘后认为对第一阶段是过度工程。

## 候选方案

### Web 框架
- A. `gin-gonic/gin`（社区最大、中间件丰富）
- B. Hertz（字节，性能好但社区资料少）
- C. GoFrame 全家桶（与 GORM 不兼容，AI 模块缺失）

### ORM / 数据库驱动
- A. GORM + `glebarez/sqlite`（纯 Go、无 CGO，跨平台编译方便）
- B. GORM + `mattn/go-sqlite3`（CGO 依赖）
- C. 原生 `database/sql`（开发效率低）

### AI 抽象
- A. **第一阶段不自建 Agent 框架**：仅 `LLMProvider` 接口 + `OpenAIProvider`（基于 `go-openai`），覆盖 OpenAI 兼容协议（DeepSeek/GLM/Kimi/通义/Ollama）
- B. 引入 `tmc/langchaingo`（维护节奏弱）
- C. 自建薄 Agent 层 + `cloudwego/eino`（YAGNI）

### 数据库迁移
- A. 第一阶段仅用 GORM `AutoMigrate`，接生产 DB 时再引入 `golang-migrate`
- B. 一开始就上 `golang-migrate`（学习曲线 + 维护成本）

### ID 生成
- A. `google/uuid`（UUID v7，自带时间序，RFC 9562 标准）
- B. `bwmarrin/snowflake`（分布式才需要）

### 报表导出
- A. 仅 `xuri/excelize/v2` + Markdown，PDF 由前端浏览器打印 / jsPDF
- B. 引入 `signintech/gopdf`（CGO 字体依赖、维护负担）

### 依赖注入
- A. `internal/bootstrap/wire.go` 手写显式装配
- B. `google/wire` 编译期 DI（< 30 组件时收益不明显）

### 金额精度
- A. `shopspring/decimal`（行业标准）
- B. `int64 ÷ 100` 自造定点（精度损失风险大）

### 日志
- A. 标准库 `slog`（Go 1.21+ 内置，零依赖）
- B. `go.uber.org/zap`（性能好但增加依赖；可在生产期通过 slog handler 桥接）

## 决策

第一阶段采纳全部 **A 方案**：

| 类别 | 选择 |
|---|---|
| Web 框架 | `gin-gonic/gin` |
| ORM | `gorm.io/gorm` |
| SQLite 驱动 | `glebarez/sqlite`（纯 Go，无 CGO） |
| AI 客户端 | `sashabaranov/go-openai` 直用，**不**自建 Agent |
| 迁移工具 | GORM `AutoMigrate` 起步 |
| ID 生成 | `google/uuid` UUID v7 |
| 报表 | `excelize/v2`（Excel）+ `text/template`（Markdown） |
| DI | 手写 `internal/bootstrap/wire.go` |
| 金额 | `shopspring/decimal` |
| 日志 | 标准库 `slog` |

完整 13 个核心库见 `docs/architecture-design.md` §2.1 和 `.ai-rd-team/memory/agent.d/tech-stack-selected.md`。

## "暂不引入"清单与触发条件

| 组件 | 推迟原因 | 触发引入条件 |
|---|---|---|
| `cloudwego/eino` | YAGNI，go-openai 原生 Tool Calling/Streaming/JSON Mode 已覆盖第一阶段需求 | 出现「多步推理 + 工具循环 + 复杂 Graph 编排」业务场景（如复杂报表 Agent） |
| `tmc/langchaingo` | 已被 Eino 替代为后续选项 | 不再考虑（如启用 Agent 框架则上 Eino） |
| `golang-migrate/migrate` | AutoMigrate 第一阶段够用 | 接 PostgreSQL / MySQL / TDSQL 时 |
| `bwmarrin/snowflake` | UUID v7 已自带时间序 | 分布式部署 + 需要更短/纯数字 ID |
| `signintech/gopdf` | CGO 字体依赖 | 用户明确要求服务端生成 PDF |
| `google/wire` | < 30 组件收益不明显 | `bootstrap/wire.go` 超过 200 行或 30 个组件 |

## 后果

✅ **正面**
- 第一阶段依赖可控（13 个核心库），编译产物小，跨平台部署方便（无 CGO）
- 业务层只依赖 7 个抽象接口（Repository / Cache / LLMProvider / EventBus / IDGenerator / Migrator / ReportExporter），切换实现仅改 `bootstrap/wire.go` 与配置
- AI 层最小化：业务 Service 直接调 `LLMProvider`，遇 ReAct 显式循环（几十行），可读、可测、可追溯
- DeepSeek / GLM / Kimi / 通义 / Ollama 等 OpenAI 兼容模型只改 `baseURL` 即可切换，无锁定

⚠️ **代价**
- ReAct/工具循环代码出现在多个 Service 时会有重复，需 reviewer 关注抽出公共 helper
- AutoMigrate 不能 rename / 改类型 / 删字段，第一阶段开发期需要约束变更方式
- `internal/bootstrap/wire.go` 手写组装在组件多时会变长（30 个时考虑切 Wire）

🔁 **重新评估触发条件**（任一满足即新写 ADR 升级）
- AI 业务出现 Multi-Agent / DAG 编排需求 → 引入 Eino，新增 `EinoProvider` 实现 `LLMProvider`
- 接入生产 PostgreSQL / MySQL / TDSQL → 引入 `golang-migrate`，把 `migrations/` 改造为 `NNNN_xxx.up.sql` / `.down.sql` 格式
- `bootstrap/wire.go` 超过 200 行或 30 个组件 → 改造为 Wire ProviderSet
- 出现纯数字 ID 业务需求或分布式部署 → 增加 `SnowflakeIDGenerator` 实现 `IDGenerator`

## 升级路径

所有"未来可能引入"的库都已经通过**接口抽象**预留好扩展点：

```
LLMProvider           → OpenAIProvider（v1）→ EinoProvider（v2）
Migrator              → AutoMigrator（v1）→ GolangMigrate（v2）
IDGenerator           → UUIDv7Generator（v1）→ SnowflakeGenerator（v2）
ReportExporter        → ExcelExporter / MarkdownExporter（v1）→ PDFExporter（v2）
CacheProvider         → LocalCache（v1）→ RedisCache（生产）
EventBus              → ChannelBus（v1）→ NatsBus / KafkaBus（v2）
Repository（GORM 统一） → SQLite（v1）→ PG / MySQL / TDSQL（无代码变更）
```

业务代码只 import 接口，升级时**仅改** `internal/bootstrap/wire.go` + 配置文件。
