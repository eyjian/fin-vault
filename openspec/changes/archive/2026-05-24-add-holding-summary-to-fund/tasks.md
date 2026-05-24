## 1. 修改数据请求

- [x] 1.1 修改 `FundManage.vue` 的 `fetchList()` 函数，添加 `include_holdings: true` 参数到 `assetApi.funds()` 调用

## 2. 添加盈亏格式化函数

- [x] 2.1 从 `StockManage.vue` 复制 `getPnlColor()` 函数到 `FundManage.vue`（处理盈亏颜色：正绿负红）
- [x] 2.2 从 `StockManage.vue` 复制 `formatPnl()` 函数到 `FundManage.vue`（格式化盈亏数值：保留2位小数，添加正负号）
- [x] 2.3 从 `StockManage.vue` 复制 `formatPnlRatio()` 函数到 `FundManage.vue`（格式化盈亏比率：转换为百分比）

## 3. 添加持仓汇总表格列

- [x] 3.1 在"净值日"列后添加"持有数量"列（对应 `row.holding_summary?.quantity`）
- [x] 3.2 添加"平均成本"列（对应 `row.holding_summary?.avg_cost`）
- [x] 3.3 添加"市值"列（对应 `row.holding_summary?.market_value`）
- [x] 3.4 添加"未实现盈亏"列（对应 `row.holding_summary?.unrealized_pnl`，使用 `getPnlColor` 和 `formatPnl`）
- [x] 3.5 添加"总盈亏"列（对应 `row.holding_summary?.total_pnl`，使用 `getPnlColor` 和 `formatPnl`）
- [x] 3.6 添加"盈亏比率"列（对应 `row.holding_summary?.pnl_ratio`，使用 `getPnlColor` 和 `formatPnlRatio`）
- [x] 3.7 添加"已实现盈亏"列（对应 `row.holding_summary?.realized_pnl`，使用 `getPnlColor` 和 `formatPnl`）
- [x] 3.8 添加"累计分红"列（对应 `row.holding_summary?.total_dividend`）

## 4. 验证

- [ ] 4.1 启动前端开发服务器，访问基金页面
- [ ] 4.2 验证有持仓的基金显示正确的盈亏数据
- [ ] 4.3 验证无持仓的基金显示 `-` 占位符
- [ ] 4.4 验证盈亏数值的正负数颜色显示正确（正：绿色，负：红色）
- [ ] 4.5 验证盈亏比率以百分比格式正确显示
