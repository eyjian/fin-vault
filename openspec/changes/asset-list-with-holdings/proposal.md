## Why

当前 `/stock`、`/wealth`、`/cash` 页面只展示资产基本信息（代码、名称、市场等），缺少持仓和盈亏数据。用户无法直观掌握资产的盈亏情况，需要跳转到其他页面才能查看持仓详情。本变更将在资产列表页面直接展示完整的持仓和盈亏数据，提升用户体验。

## What Changes

- **后端**：扩展 Asset API (`GET /assets`)，新增可选查询参数 `?include_holdings=true`，当指定该参数时，返回的资产列表中每个资产对象将包含关联的持仓和盈亏数据
- **后端**：新增 `HoldingView` 数据的预加载和计算逻辑，在 Asset Service 中集成持仓数据查询
- **前端**：更新 `/stock`、`/wealth`、`/cash` 页面，调用新的 API 参数，展示完整的持仓和盈亏数据
- **前端**：在资产列表页面新增以下展示字段：
  - A. 持有数量（股数/份额/金额）
  - B. 平均成本（每股/每份额成本）
  - C. 最新价/净值
  - D. 市值（持有数量 × 最新价）
  - E. 未实现盈亏（市值 - 总成本）
  - F. 总盈亏（未实现 + 已实现分红/利息）
  - G. 盈亏比率（%）
  - H. 已实现盈亏（卖出已获利部分）
  - I. 累计分红/利息

## Capabilities

### New Capabilities
- `asset-list-with-holdings`: 资产列表页面展示持仓和盈亏数据的能力，包括后端 API 扩展和前端页面更新

### Modified Capabilities

（无现有 capability 需要修改）

## Impact

- **后端代码**：`backend/internal/service/asset_service.go` - 需要扩展 List 方法支持预加载持仓数据
- **后端代码**：`backend/internal/domain/asset.go` - 可能需要在 Asset 模型中添加 HoldingView 字段
- **前端代码**：`frontend/src/views/asset/StockManage.vue` - 更新页面展示持仓数据
- **前端代码**：`frontend/src/views/asset/WealthManage.vue` - 更新页面展示持仓数据
- **前端代码**：`frontend/src/views/asset/CashManage.vue` - 更新页面展示持仓数据
- **前端代码**：`frontend/src/api/asset.ts` - 更新 API 调用，添加 `include_holdings` 参数
- **API 兼容性**：新增参数为可选，不影响现有 API 调用方
