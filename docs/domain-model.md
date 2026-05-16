in# FinVault（锦仓）领域模型设计

> 文档版本：v1.0  
> 创建日期：2026-05-16  
> 状态：已确认  
> 关联文档：[architecture-design.md](./architecture-design.md)、[database-schema.md](./database-schema.md)

## 1. 设计原则

本领域模型遵循以下原则：

1. **资产-持仓-交易三层分离**：Asset 描述"是什么"，Holding 描述"现在持有多少"，Transaction 描述"发生过什么"
2. **事件溯源**：所有持仓状态都可由 Transaction 流水回放计算得出，Holding 是缓存视图
3. **单一职责**：每个实体职责清晰，避免一个表承载多种语义
4. **可扩展性**：通过 `AssetType` + Detail 表的方式，新增资产类型不影响核心表
5. **多用户预留**：所有业务表都带 `user_id` 字段，第一阶段单用户固定为 1，未来开启多用户零迁移
6. **多币种原生支持**：所有金额字段记录原币种，不冗余存折算值，避免汇率波动导致数据漂移

## 2. 核心决策汇总

| 决策项 | 选择 | 说明 |
|--------|------|------|
| 资产建模方式 | 主表 + 类型扩展表 | Asset 主表 + FundDetail/StockDetail/WealthDetail 扩展，类型间字段差异大 |
| 持仓建模方式 | 独立 Holding 表 | 与 Asset 解耦，支持同一资产在多平台持有产生多条 Holding |
| 交易建模方式 | 单表多类型 | Transaction 表用 `txn_type` 字段区分买/卖/分红/到期等，便于统一查询 |
| 成本计算方法 | 移动加权平均 + FIFO 双方法 | 用户在 Holding 上配置 `cost_method`，默认加权平均 |
| 多币种处理 | 原币种存储 + 实时折算 | 增加 ExchangeRate 表存历史汇率，统计时按需折算 |
| 理财到期处理 | 自动生成 mature 流水 | 定时扫描 EndDate，自动生成到期流水并更新状态 |
| 现金账户 | 作为特殊 Asset（asset_type=cash） | 不新增 CashAccount 表，复用 Asset/Holding/Transaction |

## 3. 实体关系总览

```
┌────────┐
│  User  │ 用户（第一阶段单用户）
└───┬────┘
    │ 1:N
    ├──────────────────┬──────────────┬───────────────────┐
    ▼                  ▼              ▼                   ▼
┌────────┐      ┌────────────┐  ┌──────────────┐  ┌────────────────┐
│ Asset  │ N:1  │  Holding   │  │ Transaction  │  │ AIConversation │
│        │◄─────│            │◄─│              │  │                │
└───┬────┘      └─────┬──────┘  └──────┬───────┘  └────────┬───────┘
    │                  │                │                    │ 1:N
    │ 1:1              │ 1:N            │ N:1                ▼
    ├──► FundDetail   │                │              ┌──────────────┐
    ├──► StockDetail  │                │              │  AIMessage   │
    └──► WealthDetail │                │              └──────────────┘
                       │                │
                       │ N:1            │
                       ▼                │
                 ┌──────────┐          │
                 │ Portfolio│          │
                 └──────────┘          │
                                        │
                                  ┌─────┴─────┐
                                  │  CostLot  │ FIFO 辅助批次
                                  └───────────┘

┌────────────┐    ┌──────────────┐    ┌──────────┐    ┌────────┐
│ Platform   │    │ PriceQuote   │    │ExchgRate │    │ Report │
│（字典表）  │    │（行情快照）  │    │（汇率）  │    │（报告）│
└────────────┘    └──────────────┘    └──────────┘    └────────┘
                  ▲                                    
                  │ 引用 Asset                         
                  └────────                            
```

## 4. 核心实体定义

### 4.1 User（用户）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| username | string(64) | ✅ | 登录名，唯一索引 |
| password_hash | string(128) | ✅ | bcrypt 加密 |
| display_name | string(64) | ❌ | 显示名 |
| email | string(128) | ❌ | 邮箱 |
| default_currency | string(10) | ✅ | 默认展示币种，默认 `CNY` |
| status | string(20) | ✅ | `active`/`disabled` |
| created_at | time | ✅ | |
| updated_at | time | ✅ | |

**第一阶段**：默认创建 ID=1 的本地用户，前端不开放注册登录页面。

### 4.2 Platform（平台字典）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| code | string(32) | ✅ | 唯一编码，如 `zsbank`/`ttfund`/`futu`/`licai_tong` |
| name | string(64) | ✅ | 显示名，如"招商银行APP"、"天天基金" |
| platform_type | string(20) | ✅ | `bank`/`fund_platform`/`broker`/`internet` |
| icon_url | string(255) | ❌ | 图标 URL |
| is_system | bool | ✅ | 是否系统预置 |
| status | string(20) | ✅ | `active`/`inactive` |
| created_at | time | ✅ | |

**预置数据**（首次启动初始化）：

| code | name | platform_type |
|------|------|---------------|
| zsbank | 招商银行APP | bank |
| icbc | 工商银行APP | bank |
| abc | 农业银行APP | bank |
| ttfund | 天天基金 | fund_platform |
| futu | 富途牛牛 | broker |
| licai_tong | 理财通 | internet |
| alipay | 支付宝 | internet |
| wechat | 微信支付 | internet |

### 4.3 Asset（资产主表）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| user_id | uint | ✅ | 所属用户，第一阶段=1 |
| asset_code | string(32) | ✅ | 资产代码：基金代码 / 股票代码 / 理财产品ID / `CASH-{platform}-{currency}` |
| name | string(128) | ✅ | 资产名称 |
| asset_type | string(20) | ✅ | `fund`/`stock`/`wealth`/`cash` |
| currency | string(10) | ✅ | 计价币种，如 `CNY`/`HKD`/`USD` |
| issuer_platform_id | uint | ❌ | 发行平台 ID（理财产品的发行银行；现金的所属平台） |
| risk_level | string(20) | ❌ | `R1`/`R2`/`R3`/`R4`/`R5`，理财/基金的风险等级 |
| status | string(20) | ✅ | `active`/`delisted`/`matured` |
| remark | string(500) | ❌ | 备注 |
| created_at | time | ✅ | |
| updated_at | time | ✅ | |

**唯一索引**：`(user_id, asset_code, asset_type)`

**字段说明**：
- 同一支基金在多个平台购买，只会有 1 条 Asset 记录，多条 Holding 记录
- 现金作为特殊 Asset：`asset_type=cash`，`asset_code=CASH-zsbank-CNY` 这种格式

### 4.4 FundDetail（基金扩展）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| asset_id | uint | ✅ | 主键，关联 Asset.id |
| fund_type | string(20) | ❌ | `equity`/`bond`/`hybrid`/`money`/`index`/`qdii` |
| manager | string(64) | ❌ | 基金经理 |
| company | string(128) | ❌ | 基金公司 |
| inception_date | date | ❌ | 成立日 |
| latest_nav | decimal(20,4) | ❌ | 最新净值 |
| latest_nav_date | date | ❌ | 最新净值日期 |
| benchmark | string(255) | ❌ | 业绩比较基准 |

### 4.5 StockDetail（股票扩展）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| asset_id | uint | ✅ | 主键 |
| market | string(20) | ✅ | `SH`/`SZ`/`HK`/`US`/`BJ` |
| industry | string(64) | ❌ | 所属行业 |
| sector | string(64) | ❌ | 所属板块 |
| listing_date | date | ❌ | 上市日 |
| total_shares | decimal(20,2) | ❌ | 总股本（万股） |
| latest_price | decimal(20,4) | ❌ | 最新价 |
| latest_price_time | time | ❌ | 最新价时间 |

### 4.6 WealthDetail（理财扩展）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| asset_id | uint | ✅ | 主键 |
| product_type | string(20) | ✅ | `fixed_deposit`(定期)/`structured`(结构性)/`floating`(浮动收益)/`pension`(养老) |
| expected_yield | decimal(8,4) | ❌ | 预期年化收益率（%） |
| actual_yield | decimal(8,4) | ❌ | 实际年化收益率（到期后填写） |
| start_date | date | ❌ | 起息日 |
| end_date | date | ❌ | 到期日（关键：自动到期触发字段） |
| term_days | int | ❌ | 期限（天） |
| min_amount | decimal(20,2) | ❌ | 起购金额 |
| redemption_rule | string(255) | ❌ | 赎回规则 |
| is_auto_renewal | bool | ❌ | 是否自动续期 |

### 4.7 Holding（持仓 - 业务核心）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| user_id | uint | ✅ | 用户 ID |
| asset_id | uint | ✅ | 关联 Asset |
| platform_id | uint | ✅ | 持仓所在平台 |
| portfolio_id | uint | ❌ | 所属投资组合（可空） |
| quantity | decimal(20,8) | ✅ | 当前持仓数量（基金份额/股票股数/理财金额） |
| avg_cost | decimal(20,8) | ✅ | 移动加权平均成本（每股/每份成本） |
| total_cost | decimal(20,2) | ✅ | 累计买入成本（含手续费） |
| realized_pnl | decimal(20,2) | ✅ | 已实现盈亏 |
| total_dividend | decimal(20,2) | ✅ | 累计分红/利息 |
| cost_method | string(20) | ✅ | `weighted_avg`/`fifo`，默认 `weighted_avg` |
| first_buy_at | time | ❌ | 首次买入时间 |
| last_txn_at | time | ❌ | 最近交易时间 |
| status | string(20) | ✅ | `holding`/`closed`(全部清仓)/`matured`(理财到期) |
| created_at | time | ✅ | |
| updated_at | time | ✅ | |

**唯一索引**：`(user_id, asset_id, platform_id)`，确保"同一资产在同一平台只有一条持仓记录"。

**计算字段**（不入库，由 Service 层实时计算）：
- `market_value` = `quantity × latest_price`（最新市值）
- `unrealized_pnl` = `market_value - total_cost + realized_pnl`（浮动盈亏）
- `total_pnl` = `unrealized_pnl + realized_pnl + total_dividend`（总盈亏）
- `pnl_ratio` = `total_pnl / total_cost`（收益率）

**为什么 market_value/unrealized_pnl 不入库**：
- 行情每秒变化，入库会导致大量更新
- 由 Service 层根据 PriceQuote 实时计算，前端展示前才计算一次
- 历史快照通过定期任务写入 Report 表

### 4.8 Transaction（交易流水 - 事件流）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| user_id | uint | ✅ | 用户 ID |
| holding_id | uint | ✅ | 关联 Holding |
| asset_id | uint | ✅ | 冗余，便于按资产查询 |
| platform_id | uint | ✅ | 冗余，便于按平台查询 |
| txn_type | string(20) | ✅ | 见下方枚举表 |
| txn_time | time | ✅ | 交易发生时间 |
| quantity | decimal(20,8) | ✅ | 交易数量（卖出为正数，方向由 txn_type 决定） |
| price | decimal(20,8) | ✅ | 成交单价 |
| amount | decimal(20,2) | ✅ | 成交金额（不含费） |
| fee | decimal(20,2) | ✅ | 手续费，默认 0 |
| tax | decimal(20,2) | ✅ | 税费，默认 0 |
| net_amount | decimal(20,2) | ✅ | 净金额（买入: amount+fee+tax；卖出: amount-fee-tax） |
| currency | string(10) | ✅ | 交易币种 |
| source | string(20) | ✅ | `manual`(手动录入)/`import`(批量导入)/`auto_mature`(自动到期) |
| external_id | string(64) | ❌ | 外部系统订单号（防重） |
| note | string(500) | ❌ | 备注 |
| created_at | time | ✅ | |
| updated_at | time | ✅ | |

**唯一索引**：`(user_id, platform_id, external_id)` WHERE external_id IS NOT NULL（防止重复导入）

**txn_type 枚举**：

| txn_type | 含义 | 影响 Holding 字段 | 适用资产 |
|----------|------|-------------------|---------|
| buy | 买入 | quantity↑, total_cost↑, avg_cost 重算 | 全部 |
| sell | 卖出 | quantity↓, realized_pnl↑, total_cost↓ | 全部 |
| dividend | 现金分红/派息 | total_dividend↑ | fund, stock, wealth |
| dividend_reinvest | 分红再投资 | quantity↑, 不影响成本 | fund |
| split | 拆股/合股 | quantity 调整, avg_cost 调整 | stock |
| bonus | 送股 | quantity↑, avg_cost 摊薄 | stock |
| mature | 到期赎回 | 全量平仓，realized_pnl↑ | wealth |
| interest | 利息入账 | total_dividend↑ | wealth, cash |
| deposit | 充值（仅 cash） | quantity↑（金额=份额） | cash |
| withdraw | 提现（仅 cash） | quantity↓ | cash |
| cash_in | 现金入账（卖出回款联动） | quantity↑ | cash |
| cash_out | 现金出账（买入扣款联动） | quantity↓ | cash |
| adjust | 手动调整 | 自由调整，需要 note | 全部 |

### 4.9 PriceQuote（行情快照）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| asset_id | uint | ✅ | 关联 Asset |
| price | decimal(20,8) | ✅ | 价格/净值 |
| quote_time | time | ✅ | 报价时间 |
| change_pct | decimal(10,4) | ❌ | 当日涨跌幅（%） |
| volume | decimal(20,2) | ❌ | 成交量（股票用） |
| source | string(20) | ✅ | `manual`/`api_sina`/`api_tencent`/`api_eastmoney` |
| created_at | time | ✅ | |

**索引**：`(asset_id, quote_time DESC)`，便于查询最新价。

**用途**：
- 用户主动刷新或定时拉取，写入快照
- 计算 Holding 的 `market_value` 时取最新一条
- 历史价格用于回测、报表绘制

### 4.10 ExchangeRate（汇率快照）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| from_currency | string(10) | ✅ | 源币种，如 `HKD` |
| to_currency | string(10) | ✅ | 目标币种，通常 `CNY` |
| rate | decimal(12,6) | ✅ | 汇率：1 from = rate × to |
| quote_date | date | ✅ | 报价日期 |
| source | string(20) | ✅ | `manual`/`pboc`(央行)/`api` |
| created_at | time | ✅ | |

**唯一索引**：`(from_currency, to_currency, quote_date, source)`

**使用规则**：
- 资产折算优先使用 `quote_date <= 当前日期` 中最新的一条
- 历史报表使用"当时的汇率"（`quote_date = 报表统计日`）
- 缺失汇率时回退到 `manual` 源最近一条

### 4.11 CostLot（成本批次 - FIFO 辅助）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| holding_id | uint | ✅ | 关联 Holding |
| txn_id | uint | ✅ | 来源买入交易 |
| buy_price | decimal(20,8) | ✅ | 该批次买入价（含手续费摊销） |
| buy_time | time | ✅ | 买入时间（FIFO 排序依据） |
| original_qty | decimal(20,8) | ✅ | 原始份额 |
| remaining_qty | decimal(20,8) | ✅ | 剩余未卖份额 |
| status | string(20) | ✅ | `open`(有剩余)/`closed`(已耗尽) |
| created_at | time | ✅ | |
| updated_at | time | ✅ | |

**索引**：`(holding_id, status, buy_time)`，便于 FIFO 卖出时快速找到最早未耗尽的批次。

**第一阶段策略**：
- 表结构建好但**默认不写入**（cost_method=weighted_avg 时）
- 用户切换到 `fifo` 时，按 Holding 的所有 buy 类型 Transaction 批量回放生成 CostLot
- 卖出时按 buy_time 升序消耗 remaining_qty

### 4.12 Portfolio（投资组合）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| user_id | uint | ✅ | 用户 ID |
| name | string(64) | ✅ | 组合名称，如"养老金组合"、"激进成长" |
| description | string(500) | ❌ | 描述 |
| target_allocation | json | ❌ | 目标配置比例，如 `{"equity": 0.6, "bond": 0.3, "cash": 0.1}` |
| color | string(20) | ❌ | UI 显示颜色 |
| sort_order | int | ✅ | 显示顺序 |
| created_at | time | ✅ | |
| updated_at | time | ✅ | |

**第一阶段**：建表但 UI 暂不开放，等核心功能稳定后再开放。

### 4.13 AIConversation（AI 对话会话）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| user_id | uint | ✅ | 用户 ID |
| title | string(128) | ✅ | 会话标题（可由 AI 总结生成） |
| scene | string(20) | ✅ | `chat`(自由对话)/`buy_sell`(买卖建议)/`analysis`(盈亏分析)/`report`(报表生成) |
| llm_provider | string(32) | ✅ | 使用的模型，如 `deepseek`/`glm` |
| llm_model | string(64) | ✅ | 具体模型名，如 `deepseek-chat` |
| message_count | int | ✅ | 消息数量（冗余字段） |
| total_tokens | int | ✅ | 累计 token 使用量 |
| status | string(20) | ✅ | `active`/`archived`/`deleted` |
| created_at | time | ✅ | |
| updated_at | time | ✅ | |

### 4.14 AIMessage（AI 对话消息）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| conversation_id | uint | ✅ | 关联 AIConversation |
| role | string(20) | ✅ | `system`/`user`/`assistant`/`tool` |
| content | text | ✅ | 消息内容 |
| tool_name | string(64) | ❌ | 工具调用时的工具名 |
| tool_args | json | ❌ | 工具调用参数 |
| tool_result | text | ❌ | 工具调用结果 |
| token_count | int | ❌ | 该条消息 token 数 |
| created_at | time | ✅ | |

**索引**：`(conversation_id, created_at)`

### 4.15 Report（报表缓存）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | uint | ✅ | 主键 |
| user_id | uint | ✅ | 用户 ID |
| report_type | string(20) | ✅ | `weekly`/`monthly`/`quarterly`/`yearly`/`custom` |
| period_start | date | ✅ | 统计周期开始 |
| period_end | date | ✅ | 统计周期结束 |
| display_currency | string(10) | ✅ | 报表展示币种 |
| snapshot_data | json | ✅ | 期初/期末资产快照 |
| analysis_data | json | ✅ | 各维度统计数据 |
| ai_summary | text | ❌ | AI 生成的总结 |
| status | string(20) | ✅ | `generating`/`ready`/`failed` |
| generated_at | time | ❌ | 生成完成时间 |
| created_at | time | ✅ | |

**唯一索引**：`(user_id, report_type, period_start, period_end)`

**第一阶段**：建表，但报表生成功能在第二阶段开发。

## 5. 关键业务规则

### 5.1 买入交易处理流程

```
用户提交买入 Transaction（txn_type=buy）
  ↓
1. 校验数据（quantity > 0, price > 0, platform/asset 存在）
  ↓
2. 查找或创建 Holding（按 user_id + asset_id + platform_id）
  ↓
3. 写入 Transaction 表
  ↓
4. 更新 Holding（事务内）：
   - new_quantity = old_quantity + txn.quantity
   - new_total_cost = old_total_cost + txn.net_amount
   - new_avg_cost = new_total_cost / new_quantity
   - last_txn_at = txn.txn_time
   - 若 first_buy_at 为空，设置为 txn.txn_time
  ↓
5. 若 cost_method = fifo，同时写入 CostLot
  ↓
6. 提交事务
```

### 5.2 卖出交易处理流程（移动加权平均）

```
用户提交卖出 Transaction（txn_type=sell）
  ↓
1. 校验：quantity ≤ holding.quantity
  ↓
2. 计算成本与盈亏：
   - sold_cost = avg_cost × txn.quantity
   - this_pnl = txn.net_amount - sold_cost
  ↓
3. 写入 Transaction 表
  ↓
4. 更新 Holding：
   - new_quantity = old_quantity - txn.quantity
   - new_total_cost = old_total_cost - sold_cost
   - new_realized_pnl = old_realized_pnl + this_pnl
   - avg_cost 不变（卖出不影响加权平均）
   - 若 new_quantity = 0，status = closed
```

### 5.3 卖出交易处理流程（FIFO）

```
1. 按 buy_time 升序遍历 holding 的 CostLot（status=open）
  ↓
2. 逐批次消耗 remaining_qty：
   for each lot:
       consume_qty = min(剩余卖出量, lot.remaining_qty)
       sold_cost += consume_qty × lot.buy_price
       lot.remaining_qty -= consume_qty
       if lot.remaining_qty = 0: lot.status = closed
       剩余卖出量 -= consume_qty
       if 剩余卖出量 = 0: break
  ↓
3. 计算 this_pnl = txn.net_amount - sold_cost
  ↓
4. 更新 Holding（同 5.2，但 sold_cost 来源不同）
```

### 5.4 理财到期自动处理

```
定时任务（每天 00:30 执行）：
  ↓
SELECT * FROM t_fv_core_holdings h
  JOIN t_fv_core_assets a ON h.f_asset_id = a.f_id
  JOIN t_fv_core_wealth_details w ON a.f_id = w.f_asset_id
  WHERE a.f_asset_type = 'wealth'
    AND h.f_status = 'holding'
    AND w.f_end_date <= CURRENT_DATE
  ↓
对每条记录：
  1. 计算到期收益：
     yield = w.actual_yield ?? w.expected_yield
     mature_amount = h.total_cost × (1 + yield × w.term_days / 365)
  ↓
  2. 生成 Transaction：
     txn_type = 'mature'
     txn_time = w.end_date
     quantity = h.quantity
     price = mature_amount / h.quantity
     amount = mature_amount
     net_amount = mature_amount
     source = 'auto_mature'
     note = '系统自动生成 - 理财到期'
  ↓
  3. 更新 Holding：
     status = 'matured'
     realized_pnl += (mature_amount - h.total_cost)
     quantity = 0
  ↓
  4. 推送站内通知（可选）
```

### 5.5 多币种折算规则

```
统计接口参数: display_currency = "CNY" | "raw"

display_currency = "raw"（原币种视图）：
  按 currency 分组返回，每组独立计算 sum

display_currency = "CNY"（折算视图）：
  for each holding:
      if holding.currency = "CNY":
          value_cny = holding.market_value
      else:
          rate = ExchangeRate.find(holding.currency, "CNY", today, latest)
          value_cny = holding.market_value × rate.rate
  total = Σ value_cny
```

### 5.6 现金联动（可选功能）

第一阶段不强制启用。当用户开启"现金账户管理"时：

```
买入资产时（前端勾选"扣减现金"）：
  → 生成 2 笔 Transaction（一个事务内）：
    1. Asset 的 buy 流水
    2. 对应平台 Cash Asset 的 cash_out 流水

卖出资产时（前端勾选"回款入现金"）：
  → 生成 2 笔 Transaction：
    1. Asset 的 sell 流水
    2. 对应平台 Cash Asset 的 cash_in 流水
```

## 6. 索引与查询性能

### 6.1 关键索引

| 表 | 索引 | 用途 |
|----|------|------|
| asset | `(user_id, asset_code, asset_type)` UNIQUE | 防重 + 查询 |
| asset | `(user_id, asset_type, status)` | 按类型筛选 |
| holding | `(user_id, asset_id, platform_id)` UNIQUE | 防重 + 主查询 |
| holding | `(user_id, status, platform_id)` | 列表筛选 |
| transaction | `(user_id, holding_id, txn_time)` | 按持仓查流水 |
| transaction | `(user_id, txn_time)` | 时间范围查询 |
| transaction | `(user_id, platform_id, external_id)` UNIQUE | 防重导入 |
| price_quote | `(asset_id, quote_time DESC)` | 取最新价 |
| exchange_rate | `(from_currency, to_currency, quote_date DESC)` | 取最新汇率 |
| cost_lot | `(holding_id, status, buy_time)` | FIFO 排序 |
| ai_message | `(conversation_id, created_at)` | 历史消息加载 |

### 6.2 性能优化策略

- **持仓列表**：单次查询用 LEFT JOIN 关联 Asset + Platform + 最新 PriceQuote，避免 N+1
- **统计指标**：缓存到 CacheProvider，TTL=60s，行情更新或交易写入时主动失效
- **历史报表**：写入 Report 表持久化，避免重复计算
- **AI 上下文**：只加载最近 N 条消息（默认 20），超出部分由 LLM Memory 摘要

## 7. 数据完整性保证

### 7.1 事务边界

以下操作必须在数据库事务内完成：

- 写入 Transaction + 更新 Holding（一致性）
- 写入买入 Transaction + 写入 CostLot（FIFO 一致性）
- 现金联动的两笔 Transaction（成对生成）
- 理财到期的 Transaction + Holding 状态更新

### 7.2 数据校验

Service 层强制校验：

- `quantity > 0`、`price > 0`、`amount > 0`
- 卖出量 ≤ 当前持仓量
- 同一 `external_id` 不允许重复导入
- 现金 Asset 的 `asset_code` 必须匹配 `CASH-{platform}-{currency}` 格式
- ExchangeRate 必须有对应的 ToCurrency 才能折算

### 7.3 并发控制

- 单用户场景：第一阶段不需要锁
- 多用户场景：Holding 更新使用乐观锁（`updated_at` 版本号）或悲观锁 SELECT FOR UPDATE
- 事件回放工具：保留"按 Transaction 重新计算 Holding"的运维命令，作为兜底

## 8. 第一阶段实施范围

| 实体 | 建表 | 业务功能 |
|------|------|---------|
| User | ✅ | 默认创建 ID=1，前端不开放 |
| Platform | ✅ | 预置数据 + 增删改查 |
| Asset / FundDetail / StockDetail / WealthDetail | ✅ | 完整 CRUD |
| Holding | ✅ | 完整 CRUD + 实时计算 |
| Transaction | ✅ | 完整 CRUD（手动录入 + CSV 导入） |
| PriceQuote | ✅ | 手动刷新 + 接入东方财富/新浪 API |
| ExchangeRate | ✅ | 手动维护 + 接入央行 API（可选） |
| CostLot | ✅ 建表 | 默认不写入，UI 切换 FIFO 时启用 |
| Portfolio | ✅ 建表 | UI 暂不开放 |
| AIConversation / AIMessage | ✅ | 完整对话功能 |
| Report | ✅ 建表 | 第二阶段实现生成功能 |

## 9. 后续演进路线

| 阶段 | 主要变化 | 影响 |
|------|---------|------|
| 二阶段 | 报表生成 + 周报/月报/年报模板 | 仅 Report 表 + ReportService |
| 三阶段 | 微信小程序 | 复用 API，增加小程序登录 |
| 四阶段 | 多用户 SaaS | 启用 user_id 过滤，完善权限体系 |
| 五阶段 | 分布式部署 | 见 [upgrade-guide.md](./upgrade-guide.md) |
