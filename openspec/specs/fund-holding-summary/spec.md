## ADDED Requirements

### Requirement: 基金列表展示持仓汇总数据

基金列表页面 SHALL 在表格中展示以下持仓汇总数据列（当 `holding_summary` 数据存在时）：

1. **持有数量** (`holding_summary.quantity`)
2. **平均成本** (`holding_summary.avg_cost`)
3. **市值** (`holding_summary.market_value`)
4. **未实现盈亏** (`holding_summary.unrealized_pnl`) - 正数显示绿色，负数显示红色
5. **总盈亏** (`holding_summary.total_pnl`) - 正数显示绿色，负数显示红色
6. **盈亏比率** (`holding_summary.pnl_ratio`) - 以百分比格式显示，带正负号，正数绿色，负数红色
7. **已实现盈亏** (`holding_summary.realized_pnl`) - 正数显示绿色，负数显示红色
8. **累计分红** (`holding_summary.total_dividend`)

#### Scenario: 基金有持仓数据时展示所有汇总列

- **WHEN** 用户在基金列表页面查看已持有该基金的记录
- **THEN** 表格显示持有数量、平均成本、市值、未实现盈亏、总盈亏、盈亏比率、已实现盈亏、累计分红
- **AND** 盈亏数值根据正负使用对应颜色标识（正：绿色，负：红色）

#### Scenario: 基金无持仓数据时显示占位符

- **WHEN** 用户在基金列表页面查看未持有该基金的记录（无 `holding_summary` 数据）
- **THEN** 所有持仓汇总列显示 `-` 作为占位符

#### Scenario: 请求列表时包含持仓数据

- **WHEN** 前端调用 `assetApi.funds()` 获取基金列表
- **THEN** 请求参数 SHALL 包含 `include_holdings: true`
- **AND** 后端响应 SHALL 为每个资产返回 `holding_summary` 对象（如果有关联持仓）

#### Scenario: 盈亏比率为零或空值时不显示颜色

- **WHEN** 基金的盈亏比率（`pnl_ratio`）为 0 或空值
- **THEN** 该单元格不应用颜色样式（使用默认文本颜色）
