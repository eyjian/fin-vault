# FinVault（锦仓）数据库 Schema 设计

> 文档版本：v1.1（trpc-agent-go AI 三表替换）  
> 创建日期：2026-05-16  
> 最近更新：2026-05-17  
> 状态：草稿（与代码同步演进）  
> 关联文档：[domain-model.md](./domain-model.md)、[architecture-design.md](./architecture-design.md)

> v1.1 变更摘要（议题 `replace-ai-with-trpc-agent-go`）：
> - 删除旧表 `t_fv_ai_conversations` + 旧版 `t_fv_ai_messages`（uint 主键、含 tool_name/tool_args/tool_result/token_count 字段）
> - 新增 3 张 AI 表（基于 trpc-agent-go SDK 适配，UUID 字符串主键）：
>   - `t_fv_ai_sessions`：会话主表
>   - `t_fv_ai_messages`：消息表（仅核心字段 role/content/token_usage，不含 tool 调用细节）
>   - `t_fv_ai_agent_steps`：步骤事件流（tool_call_started / tool_call_finished / token_usage / step_boundary）
> - 报表表号从 15 调整为 16

## 1. 总览

### 1.1 表清单（按模块划分）

| 模块 | # | 表名 | 中文名 | 说明 | 第一阶段 |
|------|---|------|--------|------|---------|
| **用户体系** | 1 | `t_fv_user_users` | 用户 | 多用户预留 | ✅ |
| **基础数据** | 2 | `t_fv_dict_platforms` | 平台字典 | 银行/券商/基金平台 | ✅ |
| **资产核心** | 3 | `t_fv_core_assets` | 资产主表 | 基金/股票/理财/现金 | ✅ |
| **资产核心** | 4 | `t_fv_core_fund_details` | 基金扩展 | 1:1 关联 assets | ✅ |
| **资产核心** | 5 | `t_fv_core_stock_details` | 股票扩展 | 1:1 关联 assets | ✅ |
| **资产核心** | 6 | `t_fv_core_wealth_details` | 理财扩展 | 1:1 关联 assets | ✅ |
| **资产核心** | 7 | `t_fv_core_holdings` | 持仓 | 业务核心 | ✅ |
| **资产核心** | 8 | `t_fv_core_transactions` | 交易流水 | 事件流 | ✅ |
| **行情数据** | 9 | `t_fv_quote_price_quotes` | 行情快照 | 行情历史 | ✅ |
| **行情数据** | 10 | `t_fv_quote_exchange_rates` | 汇率快照 | 多币种折算 | ✅ |
| **资产核心** | 11 | `t_fv_core_cost_lots` | 成本批次 | FIFO 辅助 | ⚠️ 建表，默认不写入 |
| **资产核心** | 12 | `t_fv_core_portfolios` | 投资组合 | 自定义分组 | ⚠️ 建表，UI 暂不开放 |
| **AI 会话** | 13 | `t_fv_ai_sessions` | AI 会话 | 多轮对话主表（基于 trpc-agent-go） | ✅ |
| **AI 会话** | 14 | `t_fv_ai_messages` | AI 会话消息 | 仅 user/assistant/tool/system，归属 sessions | ✅ |
| **AI 会话** | 15 | `t_fv_ai_agent_steps` | Agent 运行步骤 | tool_call / token_usage / step_boundary 事件流 | ✅ |
| **报表分析** | 16 | `t_fv_report_reports` | 报表缓存 | 周报/月报/年报 | ⚠️ 建表，第二阶段开发 |

**模块划分说明**：
- **user**：用户体系相关
- **dict**：字典表、基础数据
- **core**：资产、持仓、交易等核心业务
- **quote**：行情、汇率等市场数据
- **ai**：AI 会话相关（基于 trpc-agent-go：sessions / messages / agent_steps 三表）
- **report**：报表分析相关
- **sys**：系统配置（预留）

### 1.2 命名规范

- **表名**：`t_fv_{module}_{name}` 格式，模块划分见下方表清单
- **字段名**：全小写下划线 + `f_` 前缀，如 `f_id`、`f_user_id`、`f_avg_cost`、`f_created_at`
- **主键**：统一使用 `f_id`，自增 `bigint unsigned`（GORM 的 `uint`）
- **外键**：`f_{表名单数}_id`，如 `f_user_id`、`f_asset_id`、`f_platform_id`
- **时间字段**：`f_created_at`、`f_updated_at`、`f_deleted_at`（软删除，GORM 标准）
- **金额字段**：`decimal(20,2)`（金额）/ `decimal(20,8)`（数量、单价、净值）
- **币种字段**：`varchar(10)`（ISO 4217 代码：CNY/USD/HKD/EUR/JPY...）
- **枚举字段**：`varchar(20)`（保持原值，不加前缀，如 `'stock'`、`'buy'`、`'active'`）
- **状态字段**：`varchar(20)`，常用值：`active`/`inactive`/`disabled`/`closed`/`matured`
- **索引名**：唯一索引 `uk_xxx`，普通索引 `idx_xxx`（简洁，不带模块前缀）

### 1.3 数据库兼容性

所有 SQL 同时兼容：

- **SQLite 3.35+**（第一阶段本地运行）
- **MySQL 8.0+** / **TDSQL**（生产环境）
- **PostgreSQL 14+**（备选生产环境）

兼容性处理方式：
- 使用 GORM AutoMigrate 屏蔽方言差异
- 时间类型统一用 `datetime`（SQLite/MySQL）/`timestamp`（Postgres），由 GORM 自动适配
- 不使用 MySQL 特有语法（如 `ENUM`、`SET`、`JSON_TABLE`）
- JSON 字段统一存为 `text`，应用层 marshal/unmarshal

---

## 2. 完整建表 SQL（MySQL/TDSQL 方言）

> 💡 本节给出 MySQL 方言的 DDL，作为生产环境部署的基线。本地 SQLite 由 GORM AutoMigrate 自动生成，结构等价。

```sql
-- =================================================================
-- 1. t_fv_user_users 用户表
-- =================================================================
CREATE TABLE `t_fv_user_users` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_username` varchar(64) NOT NULL COMMENT '登录名',
  `f_password_hash` varchar(128) NOT NULL COMMENT 'bcrypt 密码哈希',
  `f_display_name` varchar(64) DEFAULT NULL COMMENT '显示名',
  `f_email` varchar(128) DEFAULT NULL,
  `f_default_currency` varchar(10) NOT NULL DEFAULT 'CNY' COMMENT '默认展示币种',
  `f_status` varchar(20) NOT NULL DEFAULT '活跃' COMMENT '活跃/禁用',
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  `f_deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`f_id`),
  UNIQUE KEY `uk_username` (`f_username`),
  KEY `idx_deleted_at` (`f_deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户表';


-- =================================================================
-- 2. t_fv_dict_platforms 平台字典表
-- =================================================================
CREATE TABLE `t_fv_dict_platforms` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_code` varchar(32) NOT NULL COMMENT '唯一编码',
  `f_name` varchar(64) NOT NULL COMMENT '显示名',
  `f_platform_type` varchar(20) NOT NULL COMMENT 'bank/fund_platform/broker/internet',
  `f_icon_url` varchar(255) DEFAULT NULL,
  `f_is_system` tinyint(1) NOT NULL DEFAULT '0' COMMENT '是否系统预置',
  `f_status` varchar(20) NOT NULL DEFAULT '活跃',
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  UNIQUE KEY `uk_code` (`f_code`),
  KEY `idx_type_status` (`f_platform_type`, `f_status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='平台字典表';


-- =================================================================
-- 3. t_fv_core_assets 资产主表
-- =================================================================
CREATE TABLE `t_fv_core_assets` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_user_id` bigint unsigned NOT NULL,
  `f_asset_code` varchar(32) NOT NULL COMMENT '资产代码',
  `f_name` varchar(128) NOT NULL,
  `f_asset_type` varchar(20) NOT NULL COMMENT 'fund/stock/wealth/cash',
  `f_currency` varchar(10) NOT NULL DEFAULT 'CNY',
  `f_issuer_platform_id` bigint unsigned DEFAULT NULL COMMENT '发行平台 ID',
  `f_risk_level` varchar(20) DEFAULT NULL COMMENT 'R1-R5',
  `f_status` varchar(20) NOT NULL DEFAULT '活跃' COMMENT '活跃/已退市/已到期',
  `f_remark` varchar(500) DEFAULT NULL,
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  `f_deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`f_id`),
  UNIQUE KEY `uk_user_code_type` (`f_user_id`, `f_asset_code`, `f_asset_type`),
  KEY `idx_user_type_status` (`f_user_id`, `f_asset_type`, `f_status`),
  KEY `idx_issuer` (`f_issuer_platform_id`),
  KEY `idx_deleted_at` (`f_deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='资产主表';


-- =================================================================
-- 4. t_fv_core_fund_details 基金扩展表
-- =================================================================
CREATE TABLE `t_fv_core_fund_details` (
  `f_asset_id` bigint unsigned NOT NULL,
  `f_fund_type` varchar(20) DEFAULT NULL COMMENT 'equity/bond/hybrid/money/index/qdii',
  `f_manager` varchar(64) DEFAULT NULL,
  `f_company` varchar(128) DEFAULT NULL,
  `f_inception_date` date DEFAULT NULL,
  `f_latest_nav` decimal(20,4) DEFAULT NULL,
  `f_latest_nav_date` date DEFAULT NULL,
  `f_benchmark` varchar(255) DEFAULT NULL,
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  PRIMARY KEY (`f_asset_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='基金扩展';


-- =================================================================
-- 5. t_fv_core_stock_details 股票扩展表
-- =================================================================
CREATE TABLE `t_fv_core_stock_details` (
  `f_asset_id` bigint unsigned NOT NULL,
  `f_market` varchar(20) NOT NULL COMMENT 'SH/SZ/HK/US/BJ',
  `f_industry` varchar(64) DEFAULT NULL,
  `f_sector` varchar(64) DEFAULT NULL,
  `f_listing_date` date DEFAULT NULL,
  `f_total_shares` decimal(20,2) DEFAULT NULL COMMENT '总股本（万股）',
  `f_latest_price` decimal(20,4) DEFAULT NULL,
  `f_latest_price_time` datetime DEFAULT NULL,
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  PRIMARY KEY (`f_asset_id`),
  KEY `idx_market` (`f_market`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='股票扩展';


-- =================================================================
-- 6. t_fv_core_wealth_details 理财扩展表
-- =================================================================
CREATE TABLE `t_fv_core_wealth_details` (
  `f_asset_id` bigint unsigned NOT NULL,
  `f_product_type` varchar(20) NOT NULL COMMENT 'fixed_deposit/structured/floating/pension',
  `f_expected_yield` decimal(8,4) DEFAULT NULL COMMENT '预期年化（%）',
  `f_actual_yield` decimal(8,4) DEFAULT NULL COMMENT '实际年化（到期填）',
  `f_start_date` date DEFAULT NULL,
  `f_end_date` date DEFAULT NULL COMMENT '到期日，自动到期触发字段',
  `f_term_days` int DEFAULT NULL,
  `f_min_amount` decimal(20,2) DEFAULT NULL,
  `f_redemption_rule` varchar(255) DEFAULT NULL,
  `f_is_auto_renewal` tinyint(1) NOT NULL DEFAULT '0',
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  PRIMARY KEY (`f_asset_id`),
  KEY `idx_end_date` (`f_end_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='理财扩展';


-- =================================================================
-- 7. t_fv_core_holdings 持仓表（业务核心）
-- =================================================================
CREATE TABLE `t_fv_core_holdings` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_user_id` bigint unsigned NOT NULL,
  `f_asset_id` bigint unsigned NOT NULL,
  `f_platform_id` bigint unsigned NOT NULL,
  `f_portfolio_id` bigint unsigned DEFAULT NULL,
  `f_quantity` decimal(20,8) NOT NULL DEFAULT '0' COMMENT '当前持仓数量',
  `f_avg_cost` decimal(20,8) NOT NULL DEFAULT '0' COMMENT '加权平均成本（每份）',
  `f_total_cost` decimal(20,2) NOT NULL DEFAULT '0' COMMENT '累计买入成本（含费）',
  `f_realized_pnl` decimal(20,2) NOT NULL DEFAULT '0' COMMENT '已实现盈亏',
  `f_total_dividend` decimal(20,2) NOT NULL DEFAULT '0' COMMENT '累计分红/利息',
  `f_cost_method` varchar(20) NOT NULL DEFAULT 'weighted_avg' COMMENT 'weighted_avg/fifo',
  `f_first_buy_at` datetime DEFAULT NULL,
  `f_last_txn_at` datetime DEFAULT NULL,
  `f_status` varchar(20) NOT NULL DEFAULT '持有中' COMMENT '持有中/已关闭/已到期',
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  `f_deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`f_id`),
  UNIQUE KEY `uk_user_asset_platform` (`f_user_id`, `f_asset_id`, `f_platform_id`),
  KEY `idx_user_status_platform` (`f_user_id`, `f_status`, `f_platform_id`),
  KEY `idx_portfolio` (`f_portfolio_id`),
  KEY `idx_deleted_at` (`f_deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='持仓表';


-- =================================================================
-- 8. t_fv_core_transactions 交易流水表（事件流）
-- =================================================================
CREATE TABLE `t_fv_core_transactions` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_user_id` bigint unsigned NOT NULL,
  `f_holding_id` bigint unsigned NOT NULL,
  `f_asset_id` bigint unsigned NOT NULL COMMENT '冗余便于查询',
  `f_platform_id` bigint unsigned NOT NULL COMMENT '冗余便于查询',
  `f_txn_type` varchar(20) NOT NULL COMMENT 'buy/sell/dividend/dividend_reinvest/split/bonus/mature/interest/deposit/withdraw/cash_in/cash_out/adjust',
  `f_txn_time` datetime NOT NULL,
  `f_quantity` decimal(20,8) NOT NULL,
  `f_price` decimal(20,8) NOT NULL,
  `f_amount` decimal(20,2) NOT NULL COMMENT '不含费',
  `f_fee` decimal(20,2) NOT NULL DEFAULT '0',
  `f_tax` decimal(20,2) NOT NULL DEFAULT '0',
  `f_net_amount` decimal(20,2) NOT NULL COMMENT '买:amount+fee+tax 卖:amount-fee-tax',
  `f_currency` varchar(10) NOT NULL DEFAULT 'CNY',
  `f_source` varchar(20) NOT NULL DEFAULT '手动' COMMENT '手动/导入/自动到期',
  `f_external_id` varchar(64) DEFAULT NULL COMMENT '外部订单号防重',
  `f_note` varchar(500) DEFAULT NULL,
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  `f_deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_user_holding_time` (`f_user_id`, `f_holding_id`, `f_txn_time`),
  KEY `idx_user_time` (`f_user_id`, `f_txn_time`),
  KEY `idx_user_asset_time` (`f_user_id`, `f_asset_id`, `f_txn_time`),
  KEY `idx_user_platform_time` (`f_user_id`, `f_platform_id`, `f_txn_time`),
  UNIQUE KEY `uk_user_platform_external` (`f_user_id`, `f_platform_id`, `f_external_id`),
  KEY `idx_deleted_at` (`f_deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='交易流水';


-- =================================================================
-- 9. t_fv_quote_price_quotes 行情快照表
-- =================================================================
CREATE TABLE `t_fv_quote_price_quotes` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_asset_id` bigint unsigned NOT NULL,
  `f_price` decimal(20,8) NOT NULL,
  `f_quote_time` datetime NOT NULL,
  `f_change_pct` decimal(10,4) DEFAULT NULL COMMENT '当日涨跌幅（%）',
  `f_volume` decimal(20,2) DEFAULT NULL,
  `f_source` varchar(20) NOT NULL DEFAULT '手动',
  `f_created_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_asset_time` (`f_asset_id`, `f_quote_time` DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='行情快照';


-- =================================================================
-- 10. t_fv_quote_exchange_rates 汇率快照表
-- =================================================================
CREATE TABLE `t_fv_quote_exchange_rates` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_from_currency` varchar(10) NOT NULL,
  `f_to_currency` varchar(10) NOT NULL,
  `f_rate` decimal(12,6) NOT NULL COMMENT '1 from = rate × to',
  `f_quote_date` date NOT NULL,
  `f_source` varchar(20) NOT NULL DEFAULT '手动',
  `f_created_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  UNIQUE KEY `uk_pair_date_source` (`f_from_currency`, `f_to_currency`, `f_quote_date`, `f_source`),
  KEY `idx_pair_date` (`f_from_currency`, `f_to_currency`, `f_quote_date` DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='汇率快照';


-- =================================================================
-- 11. t_fv_core_cost_lots 成本批次表（FIFO 辅助）
-- =================================================================
CREATE TABLE `t_fv_core_cost_lots` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_holding_id` bigint unsigned NOT NULL,
  `f_txn_id` bigint unsigned NOT NULL COMMENT '来源买入交易',
  `f_buy_price` decimal(20,8) NOT NULL COMMENT '该批次买入价（含费摊销）',
  `f_buy_time` datetime NOT NULL,
  `f_original_qty` decimal(20,8) NOT NULL,
  `f_remaining_qty` decimal(20,8) NOT NULL,
  `f_status` varchar(20) NOT NULL DEFAULT 'open' COMMENT 'open/closed',
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_holding_status_time` (`f_holding_id`, `f_status`, `f_buy_time`),
  KEY `idx_txn` (`f_txn_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='成本批次（FIFO）';


-- =================================================================
-- 12. t_fv_core_portfolios 投资组合表
-- =================================================================
CREATE TABLE `t_fv_core_portfolios` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_user_id` bigint unsigned NOT NULL,
  `f_name` varchar(64) NOT NULL,
  `f_description` varchar(500) DEFAULT NULL,
  `f_target_allocation` text DEFAULT NULL COMMENT 'JSON 目标配置',
  `f_color` varchar(20) DEFAULT NULL,
  `f_sort_order` int NOT NULL DEFAULT '0',
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  `f_deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_user_sort` (`f_user_id`, `f_sort_order`),
  KEY `idx_deleted_at` (`f_deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='投资组合';


-- =================================================================
-- 13. t_fv_ai_sessions AI 会话主表（trpc-agent-go）
-- =================================================================
-- 主键 f_id 是 RFC 4122 UUID 字符串（varchar 36），由 service 层用 google/uuid 生成；
-- 不继承 BaseModel —— BaseModel 用 uint 自增主键，与 UUID 字符串关联设计不兼容。
CREATE TABLE `t_fv_ai_sessions` (
  `f_id` varchar(36) NOT NULL COMMENT 'UUID',
  `f_user_id` bigint unsigned NOT NULL,
  `f_title` varchar(128) NOT NULL DEFAULT '',
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_user_updated` (`f_user_id`, `f_updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI 会话主表';


-- =================================================================
-- 14. t_fv_ai_messages AI 会话消息表（新版，trpc-agent-go）
-- =================================================================
-- 与旧 conversations.messages 的差异：
--   - 主键 f_id 改为 UUID 字符串
--   - f_session_id 改为 UUID 字符串关联 t_fv_ai_sessions.f_id
--   - 新增 f_token_usage JSON（兼容 SQLite 的 type:json → TEXT）
--   - 删除 f_tool_name / f_tool_args / f_tool_result（拆到 t_fv_ai_agent_steps）
-- f_session_id FK 仅在 spec 层承诺，实际由 service 层级联删除（SQLite 默认
-- foreign_keys=OFF，且 GORM AutoMigrate 不生成 FK 约束）。
CREATE TABLE `t_fv_ai_messages` (
  `f_id` varchar(36) NOT NULL COMMENT 'UUID',
  `f_session_id` varchar(36) NOT NULL,
  `f_role` varchar(20) NOT NULL COMMENT 'user/assistant/tool/system',
  `f_content` text NOT NULL,
  `f_token_usage` json DEFAULT NULL COMMENT 'JSON {prompt_tokens,completion_tokens,total_tokens}',
  `f_created_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_session_created` (`f_session_id`, `f_created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI 会话消息';


-- =================================================================
-- 15. t_fv_ai_agent_steps Agent 运行步骤表（trpc-agent-go）
-- =================================================================
-- 仅追加从不更新；写入前对 f_payload JSON 做敏感字段掩码（design D7：api_key /
-- password / token / authorization 等替换为 "***"）。
--
-- 索引前缀 idx_step_ 与 t_fv_ai_messages 的 idx_session_created 区分（SQLite 索引名
-- 是库级命名空间，跨表不能同名）。
--
-- f_message_id 关联 t_fv_ai_messages.f_id（D14：step ↔ assistant message 关联）；
-- service 层预生成 assistantMessageID 并通过 ctx 注入，Runner 落 step 时使用同一 ID。
CREATE TABLE `t_fv_ai_agent_steps` (
  `f_id` varchar(36) NOT NULL COMMENT 'UUID',
  `f_session_id` varchar(36) NOT NULL,
  `f_message_id` varchar(36) NOT NULL COMMENT '关联 assistant message',
  `f_event_type` varchar(32) NOT NULL COMMENT 'tool_call_started/tool_call_finished/token_usage/step_boundary',
  `f_tool_name` varchar(64) DEFAULT NULL,
  `f_payload` json NOT NULL COMMENT 'JSON 事件载荷（已掩码敏感字段）',
  `f_created_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  KEY `idx_step_session_created` (`f_session_id`, `f_created_at`),
  KEY `idx_step_session` (`f_session_id`),
  KEY `idx_step_message` (`f_message_id`),
  KEY `idx_created` (`f_created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Agent 运行步骤事件流';


-- =================================================================
-- 16. t_fv_report_reports 报表缓存表
-- =================================================================
CREATE TABLE `t_fv_report_reports` (
  `f_id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `f_user_id` bigint unsigned NOT NULL,
  `f_report_type` varchar(20) NOT NULL COMMENT 'weekly/monthly/quarterly/yearly/custom',
  `f_period_start` date NOT NULL,
  `f_period_end` date NOT NULL,
  `f_display_currency` varchar(10) NOT NULL DEFAULT 'CNY',
  `f_snapshot_data` text NOT NULL COMMENT 'JSON 期初/期末快照',
  `f_analysis_data` text NOT NULL COMMENT 'JSON 各维度统计',
  `f_ai_summary` text DEFAULT NULL,
  `f_status` varchar(20) NOT NULL DEFAULT 'generating',
  `f_generated_at` datetime DEFAULT NULL,
  `f_created_at` datetime NOT NULL,
  `f_updated_at` datetime NOT NULL,
  PRIMARY KEY (`f_id`),
  UNIQUE KEY `uk_user_type_period` (`f_user_id`, `f_report_type`, `f_period_start`, `f_period_end`),
  KEY `idx_user_status` (`f_user_id`, `f_status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报表缓存';
```

---

## 3. GORM Model 草稿（Go 代码）

> 这部分代码作为 `internal/domain/` 目录下的 Model 草稿。**请注意**：domain 层不直接依赖 GORM，GORM tag 写在 domain 结构体上是工程权衡（GORM 对 struct 无侵入，纯 tag 不污染领域层）。如果未来希望领域层完全干净，可以拆分成 `domain.Holding`（纯结构体）+ `gormmodel.Holding`（带 tag），通过转换函数互转。

### 3.1 通用基础类型

```go
package domain

import (
    "time"
    "github.com/shopspring/decimal"
    "gorm.io/gorm"
)

// BaseModel 公共字段
type BaseModel struct {
    ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
    CreatedAt time.Time      `gorm:"not null" json:"created_at"`
    UpdatedAt time.Time      `gorm:"not null" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// === 枚举常量 ===

type AssetType string

const (
    AssetTypeFund   AssetType = "fund"
    AssetTypeStock  AssetType = "stock"
    AssetTypeWealth AssetType = "wealth"
    AssetTypeCash   AssetType = "cash"
)

type TxnType string

const (
    TxnTypeBuy              TxnType = "buy"
    TxnTypeSell             TxnType = "sell"
    TxnTypeDividend         TxnType = "dividend"
    TxnTypeDividendReinvest TxnType = "dividend_reinvest"
    TxnTypeSplit            TxnType = "split"
    TxnTypeBonus            TxnType = "bonus"
    TxnTypeMature           TxnType = "mature"
    TxnTypeInterest         TxnType = "interest"
    TxnTypeDeposit          TxnType = "deposit"
    TxnTypeWithdraw         TxnType = "withdraw"
    TxnTypeCashIn           TxnType = "cash_in"
    TxnTypeCashOut          TxnType = "cash_out"
    TxnTypeAdjust           TxnType = "adjust"
)

type CostMethod string

const (
    CostMethodWeightedAvg CostMethod = "weighted_avg"
    CostMethodFIFO        CostMethod = "fifo"
)

type HoldingStatus string

const (
    HoldingStatusHolding HoldingStatus = "持有中"
    HoldingStatusClosed  HoldingStatus = "已关闭"
    HoldingStatusMatured HoldingStatus = "已到期"
)


### 3.2 User

```go
type User struct {
    BaseModel
    Username        string `gorm:"size:64;uniqueIndex;not null" json:"username"`
    PasswordHash    string `gorm:"size:128;not null" json:"-"`
    DisplayName     string `gorm:"size:64" json:"display_name"`
    Email           string `gorm:"size:128" json:"email"`
    DefaultCurrency string `gorm:"size:10;not null;default:CNY" json:"default_currency"`
    Status          string `gorm:"size:20;not null;default:active" json:"status"`
}


### 3.3 Platform

```go
type Platform struct {
    ID           uint   `gorm:"primaryKey" json:"id"`
    Code         string `gorm:"size:32;uniqueIndex;not null" json:"code"`
    Name         string `gorm:"size:64;not null" json:"name"`
    PlatformType string `gorm:"size:20;not null;index:idx_type_status,priority:1" json:"platform_type"`
    IconURL      string `gorm:"size:255" json:"icon_url"`
    IsSystem     bool   `gorm:"not null;default:false" json:"is_system"`
    Status       string `gorm:"size:20;not null;default:active;index:idx_type_status,priority:2" json:"status"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}


### 3.4 Asset / FundDetail / StockDetail / WealthDetail

```go
type Asset struct {
    BaseModel
    UserID           uint      `gorm:"not null;uniqueIndex:uk_user_code_type,priority:1;index:idx_user_type_status,priority:1" json:"user_id"`
    AssetCode        string    `gorm:"size:32;not null;uniqueIndex:uk_user_code_type,priority:2" json:"asset_code"`
    Name             string    `gorm:"size:128;not null" json:"name"`
    AssetType        AssetType `gorm:"size:20;not null;uniqueIndex:uk_user_code_type,priority:3;index:idx_user_type_status,priority:2" json:"asset_type"`
    Currency         string    `gorm:"size:10;not null;default:CNY" json:"currency"`
    IssuerPlatformID *uint     `gorm:"index" json:"issuer_platform_id"`
    RiskLevel        string    `gorm:"size:20" json:"risk_level"`
    Status           string    `gorm:"size:20;not null;default:active;index:idx_user_type_status,priority:3" json:"status"`
    Remark           string    `gorm:"size:500" json:"remark"`

    // 关联（按需 Preload）
    FundDetail   *FundDetail   `gorm:"foreignKey:AssetID" json:"fund_detail,omitempty"`
    StockDetail  *StockDetail  `gorm:"foreignKey:AssetID" json:"stock_detail,omitempty"`
    WealthDetail *WealthDetail `gorm:"foreignKey:AssetID" json:"wealth_detail,omitempty"`
}

type FundDetail struct {
    AssetID       uint            `gorm:"primaryKey" json:"asset_id"`
    FundType      string          `gorm:"size:20" json:"fund_type"`
    Manager       string          `gorm:"size:64" json:"manager"`
    Company       string          `gorm:"size:128" json:"company"`
    InceptionDate *time.Time      `gorm:"type:date" json:"inception_date"`
    LatestNAV     decimal.Decimal `gorm:"type:decimal(20,4)" json:"latest_nav"`
    LatestNAVDate *time.Time      `gorm:"type:date" json:"latest_nav_date"`
    Benchmark     string          `gorm:"size:255" json:"benchmark"`
    CreatedAt     time.Time       `json:"created_at"`
    UpdatedAt     time.Time       `json:"updated_at"`
}

type StockDetail struct {
    AssetID         uint            `gorm:"primaryKey" json:"asset_id"`
    Market          string          `gorm:"size:20;not null;index" json:"market"`
    Industry        string          `gorm:"size:64" json:"industry"`
    Sector          string          `gorm:"size:64" json:"sector"`
    ListingDate     *time.Time      `gorm:"type:date" json:"listing_date"`
    TotalShares     decimal.Decimal `gorm:"type:decimal(20,2)" json:"total_shares"`
    LatestPrice     decimal.Decimal `gorm:"type:decimal(20,4)" json:"latest_price"`
    LatestPriceTime *time.Time      `json:"latest_price_time"`
    CreatedAt       time.Time       `json:"created_at"`
    UpdatedAt       time.Time       `json:"updated_at"`
}

type WealthDetail struct {
    AssetID         uint            `gorm:"primaryKey" json:"asset_id"`
    ProductType     string          `gorm:"size:20;not null" json:"product_type"`
    ExpectedYield   decimal.Decimal `gorm:"type:decimal(8,4)" json:"expected_yield"`
    ActualYield     decimal.Decimal `gorm:"type:decimal(8,4)" json:"actual_yield"`
    StartDate       *time.Time      `gorm:"type:date" json:"start_date"`
    EndDate         *time.Time      `gorm:"type:date;index" json:"end_date"`
    TermDays        int             `json:"term_days"`
    MinAmount       decimal.Decimal `gorm:"type:decimal(20,2)" json:"min_amount"`
    RedemptionRule  string          `gorm:"size:255" json:"redemption_rule"`
    IsAutoRenewal   bool            `gorm:"not null;default:false" json:"is_auto_renewal"`
    CreatedAt       time.Time       `json:"created_at"`
    UpdatedAt       time.Time       `json:"updated_at"`
}
```


### 3.5 Holding

```go
type Holding struct {
    BaseModel
    UserID         uint            `gorm:"not null;uniqueIndex:uk_user_asset_platform,priority:1;index:idx_user_status_platform,priority:1" json:"user_id"`
    AssetID        uint            `gorm:"not null;uniqueIndex:uk_user_asset_platform,priority:2" json:"asset_id"`
    PlatformID     uint            `gorm:"not null;uniqueIndex:uk_user_asset_platform,priority:3;index:idx_user_status_platform,priority:3" json:"platform_id"`
    PortfolioID    *uint           `gorm:"index" json:"portfolio_id"`
    Quantity       decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0" json:"quantity"`
    AvgCost        decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0" json:"avg_cost"`
    TotalCost      decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0" json:"total_cost"`
    RealizedPnL    decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0" json:"realized_pnl"`
    TotalDividend  decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0" json:"total_dividend"`
    CostMethod     CostMethod      `gorm:"size:20;not null;default:weighted_avg" json:"cost_method"`
    FirstBuyAt     *time.Time      `json:"first_buy_at"`
    LastTxnAt      *time.Time      `json:"last_txn_at"`
    Status         HoldingStatus   `gorm:"size:20;not null;default:holding;index:idx_user_status_platform,priority:2" json:"status"`

    // 关联
    Asset    *Asset    `gorm:"foreignKey:AssetID" json:"asset,omitempty"`
    Platform *Platform `gorm:"foreignKey:PlatformID" json:"platform,omitempty"`
}

// HoldingView 计算字段（不入库），由 Service 层填充后返回前端
type HoldingView struct {
    *Holding
    LatestPrice    decimal.Decimal `json:"latest_price"`
    MarketValue    decimal.Decimal `json:"market_value"`
    UnrealizedPnL  decimal.Decimal `json:"unrealized_pnl"`
    TotalPnL       decimal.Decimal `json:"total_pnl"`
    PnLRatio       decimal.Decimal `json:"pnl_ratio"`
    MarketValueCNY decimal.Decimal `json:"market_value_cny,omitempty"`
}
```


### 3.6 Transaction

```go
type Transaction struct {
    BaseModel
    UserID     uint            `gorm:"not null;index:idx_user_holding_time,priority:1;index:idx_user_time,priority:1;uniqueIndex:uk_user_platform_external,priority:1" json:"user_id"`
    HoldingID  uint            `gorm:"not null;index:idx_user_holding_time,priority:2" json:"holding_id"`
    AssetID    uint            `gorm:"not null;index:idx_user_asset_time,priority:2" json:"asset_id"`
    PlatformID uint            `gorm:"not null;index:idx_user_platform_time,priority:2;uniqueIndex:uk_user_platform_external,priority:2" json:"platform_id"`
    TxnType    TxnType         `gorm:"size:20;not null" json:"txn_type"`
    TxnTime    time.Time       `gorm:"not null;index:idx_user_holding_time,priority:3;index:idx_user_time,priority:2;index:idx_user_asset_time,priority:3;index:idx_user_platform_time,priority:3" json:"txn_time"`
    Quantity   decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"quantity"`
    Price      decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"price"`
    Amount     decimal.Decimal `gorm:"type:decimal(20,2);not null" json:"amount"`
    Fee        decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0" json:"fee"`
    Tax        decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0" json:"tax"`
    NetAmount  decimal.Decimal `gorm:"type:decimal(20,2);not null" json:"net_amount"`
    Currency   string          `gorm:"size:10;not null;default:CNY" json:"currency"`
    Source     string          `gorm:"size:20;not null;default:manual" json:"source"`
    ExternalID string          `gorm:"size:64;uniqueIndex:uk_user_platform_external,priority:3" json:"external_id"`
    Note       string          `gorm:"size:500" json:"note"`

    // 关联
    Holding *Holding `gorm:"foreignKey:HoldingID" json:"holding,omitempty"`
    Asset   *Asset   `gorm:"foreignKey:AssetID" json:"asset,omitempty"`
}
```


### 3.7 PriceQuote / ExchangeRate

```go
type PriceQuote struct {
    ID        uint            `gorm:"primaryKey" json:"id"`
    AssetID   uint            `gorm:"not null;index:idx_asset_time,priority:1" json:"asset_id"`
    Price     decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"price"`
    QuoteTime time.Time       `gorm:"not null;index:idx_asset_time,priority:2" json:"quote_time"`
    ChangePct decimal.Decimal `gorm:"type:decimal(10,4)" json:"change_pct"`
    Volume    decimal.Decimal `gorm:"type:decimal(20,2)" json:"volume"`
    Source    string          `gorm:"size:20;not null;default:manual" json:"source"`
    CreatedAt time.Time       `json:"created_at"`
}

type ExchangeRate struct {
    ID           uint            `gorm:"primaryKey" json:"id"`
    FromCurrency string          `gorm:"size:10;not null;uniqueIndex:uk_pair_date_source,priority:1;index:idx_pair_date,priority:1" json:"from_currency"`
    ToCurrency   string          `gorm:"size:10;not null;uniqueIndex:uk_pair_date_source,priority:2;index:idx_pair_date,priority:2" json:"to_currency"`
    Rate         decimal.Decimal `gorm:"type:decimal(12,6);not null" json:"rate"`
    QuoteDate    time.Time       `gorm:"type:date;not null;uniqueIndex:uk_pair_date_source,priority:3;index:idx_pair_date,priority:3" json:"quote_date"`
    Source       string          `gorm:"size:20;not null;default:manual;uniqueIndex:uk_pair_date_source,priority:4" json:"source"`
    CreatedAt    time.Time       `json:"created_at"`
}
```


### 3.8 CostLot / Portfolio

```go
type CostLot struct {
    ID           uint            `gorm:"primaryKey" json:"id"`
    HoldingID    uint            `gorm:"not null;index:idx_holding_status_time,priority:1" json:"holding_id"`
    TxnID        uint            `gorm:"not null;index" json:"txn_id"`
    BuyPrice     decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"buy_price"`
    BuyTime      time.Time       `gorm:"not null;index:idx_holding_status_time,priority:3" json:"buy_time"`
    OriginalQty  decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"original_qty"`
    RemainingQty decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"remaining_qty"`
    Status       string          `gorm:"size:20;not null;default:open;index:idx_holding_status_time,priority:2" json:"status"`
    CreatedAt    time.Time       `json:"created_at"`
    UpdatedAt    time.Time       `json:"updated_at"`
}

type Portfolio struct {
    BaseModel
    UserID           uint   `gorm:"not null;index:idx_user_sort,priority:1" json:"user_id"`
    Name             string `gorm:"size:64;not null" json:"name"`
    Description      string `gorm:"size:500" json:"description"`
    TargetAllocation string `gorm:"type:text" json:"target_allocation"` // JSON
    Color            string `gorm:"size:20" json:"color"`
    SortOrder        int    `gorm:"not null;default:0;index:idx_user_sort,priority:2" json:"sort_order"`
}
```


### 3.9 Session / Message / AgentStep / Report

> AI 三表（基于 trpc-agent-go）的 Model 草稿照搬 `backend/internal/domain/ai_session.go` 已落地实现。
> 三个 struct 均**不继承 BaseModel**：BaseModel 用 uint 自增主键 + `gorm.DeletedAt`，与 UUID 字符串主键 + 硬删的设计不兼容。

```go
// AI 三表（trpc-agent-go）
//
// FK 仅在 spec 层承诺，实际由 service 层级联删除（SQLite 默认 PRAGMA
// foreign_keys=OFF 且 GORM AutoMigrate 不生成 SQLite FK 约束）。
//
// 索引设计的关键点：
//   - Session 与 Message 主键都是 UUID 字符串，按业务 ID 查询走主键命中
//   - Message.idx_session_created (f_session_id, f_created_at)：按会话拉历史升序
//   - AgentStep 的复合索引前缀 idx_step_ 与 Message 的 idx_session_created 区分
//     （SQLite 索引名是库级命名空间，跨表不能同名）

type Session struct {
    ID        string    `gorm:"column:f_id;type:varchar(36);primaryKey" json:"id"`
    UserID    uint      `gorm:"column:f_user_id;not null;index:idx_user_updated,priority:1" json:"user_id"`
    Title     string    `gorm:"column:f_title;type:varchar(128);not null;default:''" json:"title"`
    CreatedAt time.Time `gorm:"column:f_created_at;not null" json:"created_at"`
    UpdatedAt time.Time `gorm:"column:f_updated_at;not null;index:idx_user_updated,priority:2" json:"updated_at"`
}

func (Session) TableName() string { return "t_fv_ai_sessions" }

type Message struct {
    ID         string          `gorm:"column:f_id;type:varchar(36);primaryKey" json:"id"`
    SessionID  string          `gorm:"column:f_session_id;type:varchar(36);not null;index:idx_session_created,priority:1" json:"session_id"`
    Role       string          `gorm:"column:f_role;type:varchar(20);not null" json:"role"`
    Content    string          `gorm:"column:f_content;type:text;not null" json:"content"`
    TokenUsage json.RawMessage `gorm:"column:f_token_usage;type:json" json:"token_usage,omitempty"`
    CreatedAt  time.Time       `gorm:"column:f_created_at;not null;index:idx_session_created,priority:2" json:"created_at"`
}

func (Message) TableName() string { return "t_fv_ai_messages" }

// AgentStep Agent 运行时步骤事件。
//
// EventType 取值：tool_call_started / tool_call_finished / token_usage / step_boundary
// Payload 写入前由 service 层对敏感字段做掩码（design.md D7）。
type AgentStep struct {
    ID        string          `gorm:"column:f_id;type:varchar(36);primaryKey" json:"id"`
    SessionID string          `gorm:"column:f_session_id;type:varchar(36);not null;index:idx_step_session_created,priority:1;index:idx_step_session" json:"session_id"`
    MessageID string          `gorm:"column:f_message_id;type:varchar(36);not null;index:idx_step_message" json:"message_id"`
    EventType string          `gorm:"column:f_event_type;type:varchar(32);not null" json:"event_type"`
    ToolName  string          `gorm:"column:f_tool_name;type:varchar(64)" json:"tool_name,omitempty"`
    Payload   json.RawMessage `gorm:"column:f_payload;type:json" json:"payload"`
    CreatedAt time.Time       `gorm:"column:f_created_at;not null;index:idx_step_session_created,priority:2;index:idx_created" json:"created_at"`
}

func (AgentStep) TableName() string { return "t_fv_ai_agent_steps" }

type Report struct {
    ID              uint      `gorm:"primaryKey" json:"id"`
    UserID          uint      `gorm:"not null;uniqueIndex:uk_user_type_period,priority:1;index:idx_user_status,priority:1" json:"user_id"`
    ReportType      string    `gorm:"size:20;not null;uniqueIndex:uk_user_type_period,priority:2" json:"report_type"`
    PeriodStart     time.Time `gorm:"type:date;not null;uniqueIndex:uk_user_type_period,priority:3" json:"period_start"`
    PeriodEnd       time.Time `gorm:"type:date;not null;uniqueIndex:uk_user_type_period,priority:4" json:"period_end"`
    DisplayCurrency string    `gorm:"size:10;not null;default:CNY" json:"display_currency"`
    SnapshotData    string    `gorm:"type:text;not null" json:"snapshot_data"`
    AnalysisData    string    `gorm:"type:text;not null" json:"analysis_data"`
    AISummary       string    `gorm:"type:text" json:"ai_summary"`
    Status          string    `gorm:"size:20;not null;default:generating;index:idx_user_status,priority:2" json:"status"`
    GeneratedAt     *time.Time `json:"generated_at"`
    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`
}
```

---

## 4. 初始化数据

第一次启动时自动写入（幂等，存在则跳过）：

### 4.1 默认用户

```sql
INSERT INTO t_fv_user_users (f_id, f_username, f_password_hash, f_display_name, f_default_currency, f_status, f_created_at, f_updated_at)
VALUES (1, 'admin', '$2a$10$...', '本地用户', 'CNY', '活跃', NOW(), NOW());
```

> 第一阶段单用户固定 ID=1，前端不开放注册登录页面。`f_password_hash` 使用 bcrypt 生成（即使本地阶段不验证，也保留字段，便于后期开启认证）。

### 4.2 平台字典

```sql
INSERT INTO t_fv_dict_platforms (f_code, f_name, f_platform_type, f_is_system, f_status, f_created_at, f_updated_at) VALUES
('zsbank',     '招商银行APP',  'bank',           1, '活跃', NOW(), NOW()),
('icbc',       '工商银行APP',  'bank',           1, '活跃', NOW(), NOW()),
('abc',        '农业银行APP',  'bank',           1, '活跃', NOW(), NOW()),
('ccb',        '建设银行APP',  'bank',           1, '活跃', NOW(), NOW()),
('boc',        '中国银行APP',  'bank',           1, '活跃', NOW(), NOW()),
('ttfund',     '天天基金',     'fund_platform',  1, '活跃', NOW(), NOW()),
('futu',       '富途牛牛',     'broker',         1, '活跃', NOW(), NOW()),
('tiger',      '老虎证券',     'broker',         1, '活跃', NOW(), NOW()),
('licai_tong', '理财通',       'internet',       1, '活跃', NOW(), NOW()),
('alipay',     '支付宝',       'internet',       1, '活跃', NOW(), NOW()),
('wechat',     '微信支付',     'internet',       1, '活跃', NOW(), NOW());
```

### 4.3 默认汇率（手动维护一份基础数据）

```sql
INSERT INTO t_fv_quote_exchange_rates (f_from_currency, f_to_currency, f_rate, f_quote_date, f_source, f_created_at) VALUES
('USD', 'CNY', 7.200000, CURDATE(), '手动', NOW()),
('HKD', 'CNY', 0.920000, CURDATE(), '手动', NOW()),
('EUR', 'CNY', 7.800000, CURDATE(), '手动', NOW()),
('JPY', 'CNY', 0.048000, CURDATE(), '手动', NOW());
```

---

## 5. 迁移与版本管理

### 5.1 第一阶段：GORM AutoMigrate

```go
// internal/database/migrate.go

func AutoMigrate(db *gorm.DB) error {
    return db.AutoMigrate(
        &domain.User{},
        &domain.Platform{},
        &domain.Asset{},
        &domain.FundDetail{},
        &domain.StockDetail{},
        &domain.WealthDetail{},
        &domain.Holding{},
        &domain.Transaction{},
        &domain.PriceQuote{},
        &domain.ExchangeRate{},
        &domain.CostLot{},
        &domain.Portfolio{},
        // AI 会话/消息/步骤（基于 trpc-agent-go）
        &domain.Session{},
        &domain.Message{},
        &domain.AgentStep{},
        &domain.Report{},
    )
}
```

**优点**：开发期快速，模型变更自动生成 ALTER 语句  
**限制**：AutoMigrate **不删除字段、不修改字段类型、不重命名**——只增不改

### 5.2 二阶段及后续：goose / golang-migrate

切换到显式 SQL 迁移文件管理，目录结构：

```
migrations/
├── 0001_init_schema.up.sql
├── 0001_init_schema.down.sql
├── 0002_add_portfolio_color.up.sql
├── 0002_add_portfolio_color.down.sql
└── ...
```

切换时机：
- 字段需要 rename
- 字段类型需要变更
- 需要数据回填（migration 中执行 SQL UPDATE）
- 多人协作开发，需要严格记录变更历史

### 5.3 配置示例

```yaml
# config/config.yaml
database:
  driver: sqlite              # sqlite / mysql / postgres / tdsql
  dsn: "data/finvault.db"     # SQLite 路径 / MySQL DSN / PG DSN
  auto_migrate: true          # 第一阶段开启，二阶段后关闭
  log_level: warn             # silent/error/warn/info
  max_idle_conns: 10
  max_open_conns: 50
  conn_max_lifetime: 1h
```

---

## 6. 数据完整性约束

### 6.1 应用层强校验（Service 层）

| 校验项 | 规则 | 错误码 |
|--------|------|--------|
| Quantity > 0 | 所有交易数量必须正数 | `INVALID_QUANTITY` |
| Price > 0 | 所有价格必须正数 | `INVALID_PRICE` |
| 卖出量 ≤ 持仓量 | sell/withdraw 时强制校验 | `INSUFFICIENT_QUANTITY` |
| External ID 防重 | 同 (user, platform, external_id) 不重复导入 | `DUPLICATE_TRANSACTION` |
| Cash AssetCode 格式 | 必须 `CASH-{platform}-{currency}` | `INVALID_CASH_CODE` |
| 汇率存在性 | 折算前必须有有效汇率 | `MISSING_EXCHANGE_RATE` |
| 理财字段必填 | wealth 类型必须有 WealthDetail.ProductType | `MISSING_WEALTH_DETAIL` |

### 6.2 数据库层约束

- **唯一索引**：见各表 `UNIQUE KEY` 定义
- **外键约束**：第一阶段 SQLite 不强制开启 FK（性能考虑），靠应用层保证一致性；生产环境可在 MySQL 上按需开启
- **NOT NULL**：所有不可空字段强制 NOT NULL，避免 NULL 语义混乱
- **DEFAULT 值**：金额类字段默认 0，状态字段默认 active/holding，避免 NULL

### 6.3 事务边界（必须包含在同一 DB 事务内）

| 业务操作 | 涉及表 |
|---------|--------|
| 创建交易 | `t_fv_core_transactions` + `t_fv_core_holdings`（有时还有 `t_fv_core_cost_lots`） |
| 现金联动 | 2 条 `t_fv_core_transactions` + 2 条 `t_fv_core_holdings` |
| 理财到期 | `t_fv_core_transactions` + `t_fv_core_holdings` + `t_fv_core_wealth_details`（标记） |
| 重算持仓 | `t_fv_core_holdings` + `t_fv_core_cost_lots`（按 `t_fv_core_transactions` 回放） |
| 删除持仓 | `t_fv_core_holdings`（软删）+ `t_fv_core_transactions`（保留作为审计） |

---

## 7. 性能与容量规划

### 7.1 单用户预估容量（5 年）

| 表 | 单年新增 | 5 年累计 | 备注 |
|----|---------|---------|------|
| t_fv_core_assets | ~50 | ~250 | 新买的基金/股票/理财 |
| t_fv_core_holdings | ~50 | ~250 | 与 assets 同量级 |
| t_fv_core_transactions | ~500 | ~2500 | 平均每周 10 笔 |
| t_fv_quote_prices | ~10000 | ~50000 | 主要资产每天一条 |
| t_fv_quote_exchange_rates | ~1000 | ~5000 | 4 币种 × 250 工作日 |
| t_fv_ai_messages | ~5000 | ~25000 | 每天对话产生（user/assistant 两 role） |
| t_fv_ai_agent_steps | ~50000 | 受 max_steps_size_mb 控制 | tool_call 等 4 类事件，按阈值滚动清理 |

**结论**：5 年单用户数据量 < 100 万行，SQLite 完全够用，单表查询 < 10ms。

`t_fv_ai_agent_steps` 通过 `ai.session.max_steps_size_mb`（默认 100MB，0=不清理）按 `f_created_at` 升序滚动清理，仅删步骤事件，不影响 `t_fv_ai_sessions` / `t_fv_ai_messages`（用户资产）。

### 7.2 多用户 SaaS 容量（1 万用户）

| 表 | 5 年总量 |
|----|---------|
| t_fv_core_transactions | 2500 万行 |
| t_fv_ai_messages | 2.5 亿行 |
| t_fv_ai_agent_steps | 5 亿行（按阈值清理后 << 1 亿） |
| t_fv_quote_prices | 5000 万行 |

**应对方案**（在 [upgrade-guide.md](./upgrade-guide.md) 中详述）：
- t_fv_core_transactions 按 f_user_id 哈希分表
- t_fv_ai_messages 冷数据归档到对象存储
- t_fv_ai_agent_steps 全部归档到日志数仓（运行时只保留滑动窗口）
- t_fv_quote_prices 改为时序数据库（TDengine/InfluxDB）

---

## 8. 演进检查表

引入新功能时，按以下检查表评估对 schema 的影响：

- [ ] 是否新增表？走 migration 文件，禁止直接改生产库
- [ ] 是否新增字段？AutoMigrate 阶段可直接加；migration 阶段写迁移脚本
- [ ] 是否影响唯一索引？需要审查现有数据是否冲突
- [ ] 是否需要回填数据？写 migration 时同步处理
- [ ] 是否影响枚举值？新增枚举无影响；删除枚举需要数据清洗
- [ ] 是否影响事务边界？同步更新 Service 层事务包裹
- [ ] 是否影响计算字段？同步更新 `HoldingView` 等视图结构
- [ ] 是否影响 [domain-model.md](./domain-model.md)？同步更新文档
