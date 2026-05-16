---
type: memory
layer: agent.d
author: manual
created: 2026-05-16T18:30:00+08:00
updated: 2026-05-16T18:30:00+08:00
related:
  - docs/database-schema.md
  - docs/architecture-design.md
tags: [conventions, naming, code-style]
estimated_tokens: 380
---

# 编码与命名规范（强制）

> 以下规范对所有 AI 团队成员强制生效。任何违反需在 PR/评审中被 reviewer 直接打回，无需讨论。

## 数据库命名

| 对象 | 规则 | 示例 |
|---|---|---|
| 表名 | `t_fv_{module}_{name}` 全小写复数 | `t_fv_core_holdings` / `t_fv_quote_price_quotes` |
| 字段名 | **`f_` 前缀** + 全小写下划线 | `f_id` / `f_user_id` / `f_avg_cost` / `f_created_at` |
| 主键 | 统一 `f_id` `bigint unsigned` (GORM `uint`) | — |
| 外键 | `f_{表单数}_id` | `f_user_id` / `f_asset_id` / `f_platform_id` |
| 时间 | `f_created_at` / `f_updated_at` / `f_deleted_at`（软删） | GORM 标准 |
| 唯一索引 | `uk_xxx`（不带模块前缀，简洁） | `uk_user_asset_platform` |
| 普通索引 | `idx_xxx` | `idx_user_status_platform` |

## 模块前缀（7 个）

`user` 用户 / `dict` 字典 / `core` 核心业务（资产/持仓/交易）/ `quote` 行情汇率 / `ai` AI 对话 / `report` 报表 / `sys` 系统配置（预留）

## 金额与数量类型

- **金额**：`decimal(20,2)` ↔ Go `decimal.Decimal`（`shopspring/decimal`）
- **数量/单价/净值**：`decimal(20,8)`
- **币种**：`varchar(10)` ISO 4217 代码（`CNY` / `USD` / `HKD` / `EUR` / `JPY`）
- ❌ 禁用 `float32` / `float64` / `int64 ÷ 100` 自造定点
- ❌ 禁止把 `decimal.Decimal` 序列化为 `float`（JSON 应保持字符串）

## 枚举

- 数据库存 `varchar(20)` 原值（如 `'buy'` / `'fund'` / `'active'`）
- Go 端定义类型别名常量（如 `TxnTypeBuy TxnType = "buy"`）
- ❌ 禁用 MySQL `ENUM` / `SET`

## 包导入约束（强制）

| 层 | 允许 import | 禁止 import |
|---|---|---|
| `internal/domain/` | 标准库 + `decimal` + `gorm`（仅 tag/DeletedAt） | 业务其他包 |
| `internal/repository/interfaces.go` | `context` + `domain` | `gorm` / 任何驱动 |
| `internal/repository/gorm/` | `gorm` + `domain` + `repository` 接口 | 业务 service / handler |
| `internal/service/` | `domain` + 各 `interfaces`（`repository` / `cache` / `llm`） | `gorm` / `go-redis` / `go-openai`（**绝对禁止**） |
| `internal/handler/` | `gin` + `service` 接口 | 直接操作 DB / Cache / LLM |
| `internal/llm/` | `go-openai` | 业务 service / repository / handler |

## 错误处理

- 业务错误：定义 `pkg/errs/codes.go` 错误码 + `errs.New(code, msg)` 包装
- 数据库错误不暴露到 Handler 层（在 Service 层翻译为业务错误）
- Gin 用统一中间件 Recovery + 响应封装（`pkg/utils/response.go`）

## 事务边界（强制）

以下操作必须包在**单个 GORM 事务**内：
- 写 Transaction + 更新 Holding
- 写 buy Transaction + 写 CostLot（FIFO）
- 现金联动两笔 Transaction
- 理财到期：Transaction + Holding + WealthDetail 标记

## 提交信息

中文，主题行 ≤ 50 字。模块前缀建议：`feat(asset):` / `fix(holding):` / `refactor(llm):` / `docs:` / `chore:` / `test:`
