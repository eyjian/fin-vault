## Why

基金页面（/fund）缺少持仓汇总数据列（如盈亏、市值、持有数量等），而股票、理财、现金页面均已展示这些数据。这导致用户在基金页面无法快速了解持仓盈亏情况，需要跳转到其他页面才能查看，影响用户体验的一致性。

后端 API 已完全支持为所有资产类型返回 `holding_summary` 数据，只需前端正确请求并展示即可。

## What Changes

- **修改 FundManage.vue**：
  - `fetchList()` 调用时添加 `include_holdings: true` 参数
  - 在列表表格中添加以下持仓汇总列：
    - 持有数量
    - 平均成本
    - 市值
    - 未实现盈亏（带颜色标识）
    - 总盈亏（带颜色标识）
    - 盈亏比率（带颜色标识）
    - 已实现盈亏（带颜色标识）
    - 累计分红

- **参考实现**：参照 StockManage.vue 的表格列定义和格式化逻辑（`getPnlColor`、`formatPnl`、`formatPnlRatio` 函数）

## Capabilities

### New Capabilities
- `fund-holding-summary`: 基金列表展示持仓汇总数据（持有数量、成本、市值、盈亏等）

### Modified Capabilities

（无，此变更不涉及现有 spec 的行为变更）

## Impact

- **前端代码**：`frontend/src/views/asset/FundManage.vue`
- **API 调用**：`assetApi.funds()` 添加 `include_holdings: true` 参数
- **用户体验**：基金页面与其他资产类型页面保持一致的盈亏展示能力

> **注意**：后端无需修改，所有必需接口和数据结构已就绪。
