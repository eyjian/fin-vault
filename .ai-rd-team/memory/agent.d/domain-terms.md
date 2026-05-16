---
type: memory
layer: agent.d
author: manual
created: 2026-05-16T18:30:00+08:00
updated: 2026-05-16T18:30:00+08:00
related:
  - docs/domain-model.md
tags: [domain, glossary]
estimated_tokens: 520
---

# 业务术语表（fin-vault）

> 完整定义见 `docs/domain-model.md`。本表给出各成员日常沟通必须统一的核心术语。

## 三层资产模型（核心思想）

| 概念 | 描述 | 表 |
|---|---|---|
| **Asset**（资产） | "是什么"——一支基金/一只股票/一笔理财产品/一个现金账户 | `t_fv_core_assets` |
| **Holding**（持仓） | "现在持有多少"——同一 Asset 在不同 Platform 各一条 | `t_fv_core_holdings` |
| **Transaction**（交易流水） | "发生过什么"——事件溯源，所有持仓状态可由流水回放 | `t_fv_core_transactions` |

> Holding 是**缓存视图**，不是源真相。源真相是 Transaction 流水。

## 资产类型（AssetType 枚举）

`fund` 基金 / `stock` 股票 / `wealth` 理财 / `cash` 现金（特殊 Asset，asset_code = `CASH-{platform}-{currency}`）

## 交易类型（TxnType 枚举，13 种）

| 类型 | 含义 | Holding 影响 |
|---|---|---|
| `buy` | 买入 | quantity↑ total_cost↑ avg_cost 重算 |
| `sell` | 卖出 | quantity↓ realized_pnl↑ total_cost↓ |
| `dividend` | 现金分红/派息 | total_dividend↑ |
| `dividend_reinvest` | 分红再投 | quantity↑（不影响成本） |
| `split` | 拆股/合股 | quantity 调整 + avg_cost 调整 |
| `bonus` | 送股 | quantity↑ avg_cost 摊薄 |
| `mature` | 理财到期 | 全量平仓 + realized_pnl |
| `interest` | 利息入账 | total_dividend↑ |
| `deposit` / `withdraw` | 现金充提 | cash 持仓 quantity 调整 |
| `cash_in` / `cash_out` | 现金联动 | 与 buy/sell 成对生成 |
| `adjust` | 手动调整 | 自由调整，需 note |

## 计算字段（不入库，Service 层实时算）

- `market_value` = `quantity × latest_price`
- `unrealized_pnl` = `market_value − total_cost + realized_pnl`
- `total_pnl` = `unrealized_pnl + realized_pnl + total_dividend`
- `pnl_ratio` = `total_pnl / total_cost`

## 成本方法（CostMethod）

`weighted_avg`（默认，移动加权平均）/ `fifo`（启用时写 `t_fv_core_cost_lots` 批次）

## 多币种

- 所有金额按**原币种存储**，不冗余折算值
- `t_fv_quote_exchange_rates` 存历史汇率
- 折算时取 `quote_date <= 统计日` 中最新一条
- 前端 `display_currency` 可选 `raw`（原币种分组）/ `CNY`（折算）

## 事件溯源关键不变量

> 任何 Holding 都能由其全部 Transaction 流水**重新计算得出**。运维兜底命令：「按 Transaction 重放重算 Holding」。

## 关键平台预置（`t_fv_dict_platforms`）

`zsbank`(招行) · `icbc`(工行) · `abc`(农行) · `ttfund`(天天基金) · `futu`(富途) · `licai_tong`(理财通) · `alipay` · `wechat` 等。

## 多用户预留

所有业务表带 `f_user_id`。第一阶段固定 = 1，前端不开放注册登录页。
