# 资产列表页面展示持仓和盈亏数据 - 需求文档

## 1. 背景与动机

当前 fin-vault 系统的 `/stock`（股票）、`/wealth`（理财）、`/cash`（现金）页面只展示资产基本信息（代码、名称、市场等），缺少持仓和盈亏数据。用户无法直观掌握资产的盈亏情况，需要跳转到其他页面才能查看持仓详情。

**目标**：在资产列表页面直接展示完整的持仓和盈亏数据，提升用户体验。

## 2. 需求概述

### 2.1 后端需求

扩展 Asset API (`GET /assets`)，新增可选查询参数 `?include_holdings=true`：

- 当指定该参数时，返回的资产列表中每个资产对象将包含关联的持仓和盈亏数据
- 参数为可选，不影响现有 API 调用方
- 返回字段包括：
  - `holding_quantity` - 持有数量（股数/份额/金额）
  - `holding_avg_cost` - 平均成本（每股/每份额成本）
  - `holding_latest_price` - 最新价/净值
  - `holding_market_value` - 市值（持有数量 × 最新价）
  - `holding_unrealized_pnl` - 未实现盈亏（市值 - 总成本）
  - `holding_total_pnl` - 总盈亏（未实现 + 已实现分红/利息）
  - `holding_pnl_ratio` - 盈亏比率（%）
  - `holding_realized_pnl` - 已实现盈亏（卖出已获利部分）
  - `holding_total_dividend` - 累计分红/利息

### 2.2 前端需求

更新 `/stock`、`/wealth`、`/cash` 页面，调用新的 API 参数，展示完整的持仓和盈亏数据：

**股票页面 (`/stock`)**：
- 展示列：代码、名称、市场、行业、板块、最新价、持有数量、平均成本、市值、未实现盈亏、总盈亏、盈亏比率、已实现盈亏、累计分红、平台、币种、状态

**理财页面 (`/wealth`)**：
- 展示列：代码、名称、类型、预期年化、起息/到期日、期限、持有份额、平均成本、最新净值、市值、未实现盈亏、总盈亏、盈亏比率、已实现盈亏、累计利息、平台、自动续期、状态

**现金页面 (`/cash`)**：
- 展示列：编码、名称、持有金额、关联账户、收益率、总收益、平台、币种、状态

### 2.3 边界情况处理

- 资产无持仓记录时，相关字段显示 "-" 或 "0"
- 混合资产（有持仓和无持仓）时，正确显示每行数据
- API 响应时间要求：不带 `include_holdings` 参数时 < 100ms

## 3. 技术实现指南

### 3.1 后端实现建议

**现有资源**：
- 后端已有完整的持仓计算逻辑：`HoldingView` 模型包含所需所有字段
- `backend/internal/domain/asset.go` - Asset 模型定义
- `backend/internal/service/holding_service.go` - 持仓服务，已有 `GetHoldingView` 方法
- `backend/internal/service/asset_service.go` - 资产服务，需要修改 `List` 方法

**实现步骤**：
1. 在 `AssetService.List` 方法中添加 `includeHoldings` 参数
2. 当 `includeHoldings=true` 时，预加载关联的 Holding 数据
3. 调用 `HoldingService` 计算盈亏数据
4. 将 `HoldingView` 数据合并到 Asset 响应中

### 3.2 前端实现建议

**现有资源**：
- `frontend/src/views/asset/StockManage.vue` - 股票管理页面
- `frontend/src/views/asset/WealthManage.vue` - 理财管理页面
- `frontend/src/views/asset/CashManage.vue` - 现金管理页面
- `frontend/src/api/asset.ts` - 资产 API 调用

**实现步骤**：
1. 修改 `frontend/src/api/asset.ts`，在 `getAssetList` 函数中添加 `include_holdings` 参数
2. 更新三个页面组件，在 `fetchData` 时传递 `include_holdings=true`
3. 修改表格列定义，添加持仓和盈亏相关列
4. 使用合适的格式化（如绿色/红色显示盈亏）

## 4. 验收标准

### 4.1 功能验收

- [ ] `GET /assets?asset_type=stock&include_holdings=true` 返回持仓和盈亏数据
- [ ] `GET /assets?asset_type=wealth&include_holdings=true` 返回持仓和盈亏数据
- [ ] `GET /assets?asset_type=cash&include_holdings=true` 返回持仓和盈亏数据
- [ ] 不传递 `include_holdings` 参数时，API 行为不变
- [ ] 前端三个页面正确展示所有持仓和盈亏字段
- [ ] 无持仓的资产显示 "-" 或 "0"

### 4.2 性能验收

- [ ] 不带 `include_holdings` 参数时，API 响应时间 < 100ms
- [ ] 带 `include_holdings=true` 时，API 响应时间合理（< 500ms）

### 4.3 代码质量验收

- [ ] 代码符合项目现有代码规范
- [ ] 后端添加单元测试
- [ ] 前端添加必要的错误处理

## 5. 参考资料

- 项目仓库：`/data/workspace/github/eyjian/fin-vault`
- 数据库 schema：`docs/database-schema.md`
- 架构设计：`docs/architecture-design.md`
- OpenSpec 提议：`openspec/changes/asset-list-with-holdings/`
- 后端领域模型：`backend/internal/domain/asset.go`
- 后端服务层：`backend/internal/service/`

## 6. 给 ai-rd-team 的提示词

```
请基于 fin-vault 项目，实现"资产列表页面展示持仓和盈亏数据"功能。

完整需求文档位于：docs/requirements/asset-list-with-holdings-requirement.md

请按照以下步骤执行：
1. 阅读需求文档，理解需求和现有技术架构
2. 阅读 openspec/changes/asset-list-with-holdings/ 下的 proposal.md 和 specs/**
3. 设计技术方案（如果需求文档中的技术实现指南不够详细）
4. 实现后端代码：扩展 Asset API，支持 include_holdings 参数
5. 实现前端代码：更新 /stock、/wealth、/cash 页面
6. 添加必要的测试
7. 验证功能符合验收标准

项目技术栈：
- 后端：Go + tRPC-Go
- 前端：Vue 3 + TypeScript
- 数据库：MySQL（通过 GORM）
```
