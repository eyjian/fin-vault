---
type: memory
layer: agent.d
author: manual
created: 2026-05-16T18:30:00+08:00
updated: 2026-05-16T18:30:00+08:00
related:
  - docs/architecture-design.md
  - .ai-rd-team/memory/decisions/0001-tech-stack-selection.md
tags: [tech-stack, dependencies, decisions]
estimated_tokens: 480
---

# 已选定技术栈（v2.1 红队复盘定稿）

## 后端（Go 1.21+）

| 用途 | 库 | 强约束 |
|---|---|---|
| Web 框架 | `gin-gonic/gin` | 不得换为 Hertz / GoFrame |
| 参数校验 | `go-playground/validator/v10` | Gin 自带 |
| ORM | `gorm.io/gorm` | 跨 SQLite/PG/MySQL/TDSQL 统一抽象 |
| SQLite 驱动 | `glebarez/sqlite` | **纯 Go，无 CGO**（关键：跨平台编译） |
| Redis | `redis/go-redis/v9` | 仅生产用，本地用 sync.Map+TTL |
| AI 客户端 | `sashabaranov/go-openai` | **业务层不得直接 import**，只能经 `internal/llm` |
| 配置 | `spf13/viper` | YAML + 环境变量覆盖 |
| **金额** | **`shopspring/decimal`** | **禁止 float64/float32/int64 自造定点** |
| 日志 | 标准库 `slog` | 第一版结构化日志，未来可桥接 zap |
| 定时任务 | `robfig/cron/v3` | 理财到期扫描 |
| HTTP 客户端 | `go-resty/resty/v2` | 行情/平台 API 对接 |
| 协程池 | `panjf2000/ants/v2` | 行情批量拉取并发控制 |
| ID 生成 | `google/uuid`（UUID v7） | 自带时间序，**抽象为 IDGenerator** |
| 认证 | `golang-jwt/jwt/v5` | 多用户阶段启用 |
| Excel | `xuri/excelize/v2` | PDF 由前端浏览器打印 |
| 测试 | `stretchr/testify` + `DATA-DOG/go-sqlmock` | 不得引入其他测试框架 |

## 前端

Vue 3（用户确认）。具体 UI 库待 architect 在第一次设计时选定，写入 ADR。

## 不引入清单（已论证推迟，需新引入必先写 ADR）

`cloudwego/eino` · `tmc/langchaingo` · `golang-migrate/migrate` · `bwmarrin/snowflake` · `signintech/gopdf` · `google/wire` · 各厂商 Go Agent SDK · GoFrame 全家桶 · Hertz

## AI 层关键决策（重要）

第一阶段 **不自建 Agent 框架**：

1. 只定义 `LLMProvider` 接口 + `OpenAIProvider` 实现（基于 `go-openai`）
2. 业务层调用 `provider.Chat / StreamChat / ChatWithTools`，不再包薄 Agent
3. 多步推理 / Tool Calling 循环写在 Service 层（最多 ReAct 几十行）
4. 多模型切换：`LLMRegistry.GetProvider(name)` 按配置或请求参数路由
5. DeepSeek / GLM / Kimi / 通义 / Ollama 都走 OpenAI 协议，**只改 baseURL**

## 升级最小改动的 7 个抽象接口

业务代码只依赖**接口**，不依赖具体实现：

`Repository` · `CacheProvider` · `LLMProvider` · `EventBus` · `IDGenerator` · `Migrator` · `ReportExporter`

具体实现在 `internal/bootstrap/wire.go` 集中组装；切换实现只改这一处 + 配置文件。
