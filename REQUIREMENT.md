---
title: fin-vault 项目需求总入口（给 AI 团队的输入）
version: v1.0-bootstrap
status: phase-0 接入中（团队尚未启动）
last_updated: 2026-05-16
maintainer: 主理人
---

# fin-vault 项目需求总入口

> 本文档是 ai-rd-team 数字人研发团队的**总入口**：架构师/开发/测试都从这里读起，再按需深入 `docs/` 三件套。
>
> 文档版本：v1.0 · 创建日期：2026-05-16 · 与 `docs/architecture-design.md` v2.1 对齐

---

## 1. 项目一句话

**本地起步、升级改动最小**的个人理财管理系统：把分散在多家银行 / 券商 / 基金平台的资产（基金、股票、理财、现金，含多币种）集中记录，支持手动录入与批量导入，提供持仓总览、盈亏分析、买卖建议、智能问答与周/月/年报，所有 AI 能力可在 DeepSeek / GLM / Kimi / 通义千问 / Ollama 等多模型之间切换。

## 2. 第一阶段范围（必须做）

| # | 能力 | 说明 |
|---|------|------|
| 1 | 资产记录 | 基金 / 股票 / 理财 / 现金，多平台、多币种，手动 + CSV 导入 |
| 2 | 持仓视图 | 实时市值、盈亏（已实现 / 浮动 / 总）、收益率、按平台 / 类型 / 币种聚合 |
| 3 | 交易流水 | `buy/sell/dividend/dividend_reinvest/split/bonus/mature/interest/deposit/withdraw/cash_in/cash_out/adjust` 13 种类型 |
| 4 | 行情接入 | 手动刷新 + 东方财富 / 新浪 / 腾讯 公开 API（基金净值、股票行情） |
| 5 | 多币种折算 | `ExchangeRate` 表存历史汇率，前端可选「原币种」/「CNY 折算」视图 |
| 6 | 理财到期 | 每天 00:30 定时扫描，自动生成 `mature` 流水并更新持仓状态 |
| 7 | AI 对话 | 单 Provider Tool Calling + SSE 流式输出；4 个场景：自由问答 / 盈亏分析 / 买卖建议 / 持仓建议 |
| 8 | 多模型切换 | `LLMRegistry` 按配置或请求参数路由 |
| 9 | 数据导出 | Excel（excelize/v2）+ Markdown；PDF 由前端浏览器打印解决 |

## 3. 第一阶段不做（明确推迟）

- ❌ 报表生成（周报/月报/年报）—— 表已建，功能放第二阶段
- ❌ 微信小程序 —— 第三阶段
- ❌ 多用户 / SaaS —— 第四阶段（`f_user_id` 已预留）
- ❌ 分布式部署 —— 第五阶段，详见 `docs/upgrade-guide.md`
- ❌ Eino / langchaingo —— 出现「多步推理 + 工具循环 + 复杂编排」需求才引入
- ❌ `golang-migrate` —— 接 PostgreSQL/MySQL/TDSQL 时再引入
- ❌ Wire DI —— `internal/bootstrap/` 超过 30 个组件再引入
- ❌ Snowflake ID —— UUID v7 已够用，分布式才考虑
- ❌ `gopdf` —— PDF 由前端浏览器打印
- ❌ Multi-Agent / 复杂 Graph 编排 / RAG —— 业务需求暂不需要

## 4. 关键技术栈（v2.1 红队复盘定稿）

> 完整 13 个核心库 + 决策理由见 `docs/architecture-design.md` §2。

| 层 | 选型 |
|---|------|
| 后端语言 | Go 1.21+（标准库 `slog`） |
| Web 框架 | `gin-gonic/gin` |
| 校验 | `go-playground/validator/v10` |
| ORM | `gorm.io/gorm` |
| 数据库（本地） | SQLite via `glebarez/sqlite`（纯 Go，无 CGO） |
| 数据库（生产） | PostgreSQL / MySQL / TDSQL（GORM 切换零代码改动） |
| 缓存（本地） | 进程内 `sync.Map + TTL` |
| 缓存（生产） | `redis/go-redis/v9` |
| AI 客户端 | `sashabaranov/go-openai`（覆盖 DeepSeek / GLM / Kimi / 通义 / Ollama） |
| 配置 | `spf13/viper` |
| 金额精度 | `shopspring/decimal` |
| 定时任务 | `robfig/cron/v3` |
| HTTP 客户端 | `go-resty/resty/v2` |
| 协程池 | `panjf2000/ants/v2` |
| ID 生成 | `google/uuid`（UUID v7） |
| 认证 | `golang-jwt/jwt/v5` |
| Excel 导出 | `xuri/excelize/v2` |
| 测试 | `stretchr/testify` + `DATA-DOG/go-sqlmock` |
| 前端 | Vue 3 |

## 5. 分层架构与抽象接口

```
Vue3 前端
   │
   ▼
Gin Router
   │
   ▼
Handler 层（HTTP，参数校验）
   │
   ▼
Service 层（业务编排 + AI Tool Calling 循环）
   │
   ▼
抽象接口层（业务代码只依赖这一层）
  Repository / CacheProvider / LLMProvider / EventBus
  IDGenerator / Migrator / ReportExporter
   │
   ▼
具体实现层
  GORM (sqlite/pg/mysql) │ go-redis │ go-openai │ channel │ uuid │ AutoMigrate │ excelize
```

**升级最小改动的核心保障**：所有可能更换的实现都先抽象成接口，业务层只 import 抽象，具体实现在 `internal/bootstrap/wire.go` 里集中组装。

## 6. 命名规范（强制）

> 完整规范见 `docs/database-schema.md` §1.2、`.ai-rd-team/memory/agent.d/coding-conventions.md`。

| 对象 | 规则 | 示例 |
|---|---|---|
| 表名 | `t_fv_{module}_{name}` | `t_fv_core_holdings` |
| 字段名 | `f_` 前缀 + 小写下划线 | `f_id` / `f_user_id` / `f_avg_cost` |
| 唯一索引 | `uk_xxx` | `uk_user_asset_platform` |
| 普通索引 | `idx_xxx` | `idx_user_status_platform` |
| 主键 | 统一 `f_id` | `bigint unsigned` / GORM `uint` |
| 时间字段 | `f_created_at` / `f_updated_at` / `f_deleted_at` | GORM 软删除 |
| 金额字段 | `decimal(20,2)` | 不允许 `float` |
| 数量/单价 | `decimal(20,8)` | 高精度场景 |
| 模块前缀 | `user / dict / core / quote / ai / report / sys` | 7 个模块 |

## 7. 文档地图

| 文档 | 用途 | 读者 |
|---|---|---|
| `REQUIREMENT.md`（本文件） | 总入口与范围 | 所有人 |
| `docs/architecture-design.md` | 架构、技术选型、目录结构、`bootstrap.Wire` 流程 | architect / developer / devops |
| `docs/domain-model.md` | 15 个实体定义、业务规则、事务边界、索引 | architect / developer |
| `docs/database-schema.md` | 完整建表 SQL + GORM Model 草稿 | developer |
| `docs/upgrade-guide.md` | 本地→分布式升级路径与改造检查表 | architect |
| `.ai-rd-team/memory/agent.d/` | 团队启动强制加载的核心背景 | 所有 AI 团队成员 |
| `.ai-rd-team/memory/decisions/` | ADR 关键决策追溯 | 所有人 |
| `.ai-rd-team/skills/` | 项目级 skill 覆盖（Go-Gin / 命名 / decimal / LLM Provider 等） | 所有 AI 团队成员 |

## 8. 给 ai-rd-team 团队的工作约定

1. **只读源 + 可写产出区分明确**：源文档（`docs/*.md`、`REQUIREMENT.md`）只读；代码产出落到项目根（`backend/`、`frontend/`），过程数据落到 `.ai-rd-team/runtime/`
2. **决策必留 ADR**：架构选型、库引入 / 推迟、设计权衡 → 写到 `.ai-rd-team/memory/decisions/000X-*.md`
3. **项目级 skill 优先级最高**：`workspace > global > builtin`，与 builtin 同名即可覆盖（如用 `go-gin-development.md` 覆盖 builtin 的 `go-kratos-backend`）
4. **YAGNI**：第一阶段范围（§2）以外的能力一律不做，引入新依赖前先到 `decisions/` 写 ADR 论证
5. **金额永远用 `decimal.Decimal`**：禁止 `float64` / `float32`，禁止 `int64 ÷ 100` 这类自造定点
6. **业务层只依赖接口**：`service/` 不得直接 import GORM / go-redis / go-openai 包，只 import `internal/repository`、`internal/cache`、`internal/llm`

---

## 9. 团队装配（已在 config 中配好，仅供参考）

| 角色 | 实例数 | 注入的 Skills（项目级 + builtin） |
|------|-------|----------------------------------|
| architect | 1 | `builtin:code-review-checklist` + `fin-vault-go-gin` + `fin-vault-naming` |
| developer | 2 | `fin-vault-go-gin` + `fin-vault-naming` + `fin-vault-decimal` |
| reviewer | 1 | `builtin:code-review-checklist` + `fin-vault-error-handling` + `fin-vault-llm-provider` |
| tester | 1 | `fin-vault-go-gin` + `fin-vault-decimal` |

> ⚠️ 团队会**完全替换** ai-rd-team 默认的 Python 规范 skills（`python-best-practices` / `pytest-guide`）。如果在 prompt 中看到 Python 内容，说明配置加载失败，立即停止并报告。

---

## 10. 工作分发约定

- 架构师在 `runtime/` 内产出 `data-task-breakdown.yaml`，将 M1 拆为 N 个并行任务，分配给 `developer_1` / `developer_2`
- 开发者完成代码后通过 `send_message` 通知 reviewer，**不需要经过 main**
- reviewer 的 issue 列表写入 `runtime/review/data-review-issues-{module}.yaml`，与 developer 直接讨论
- tester 等代码通过 review 后再写测试，避免对未稳定代码反复打补丁
- 任何成员遇到无法在 30 分钟内解决的卡点，先找 architect / reviewer / pm，**最后**才升级到 main（用户）

---

## 11. 更多

- 接入方案与 6 份项目级 skill 设计：`docs/ai-rd-team-onboarding.md`
- 升级路径：`docs/upgrade-guide.md`
- 决策记录：`.ai-rd-team/memory/decisions/`
