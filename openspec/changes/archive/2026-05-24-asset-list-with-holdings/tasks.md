## 1. 后端 Domain 与 Service 层

- [x] 1.1 在 `backend/internal/domain/asset.go` 中为 `Asset` 添加 `HoldingSummary *HoldingSummary` 字段（带 `gorm:"-" json:"holding_summary,omitempty"` tag）
- [x] 1.2 在 `backend/internal/domain/asset.go` 中定义 `HoldingSummary` 结构体（quantity / avg_cost / total_cost / realized_pnl / total_dividend / latest_price / market_value / unrealized_pnl / total_pnl / pnl_ratio）
- [x] 1.3 在 `backend/internal/service/asset_service.go` 的 `AssetListInput` 中增加 `IncludeHoldings bool` 字段
- [x] 1.4 在 `AssetService` 构造函数中注入 `holdingSvc *HoldingService`（允许为 nil）
- [x] 1.5 在 `AssetService.List` 内：`IncludeHoldings && holdingSvc != nil` 时遍历结果调 `holdingSvc.GetSummaryByAsset` 并赋值到 `list[i].HoldingSummary`

## 2. 后端 Handler 层

- [x] 2.1 在 `backend/internal/handler/asset_handler.go` 的 List handler 中解析 `c.Query("include_holdings") == "true"` 并写入 `AssetListInput.IncludeHoldings`
- [x] 2.2 在 `backend/internal/bootstrap/wire.go`（或对应装配文件）中把 `holdingSvc` 注入 `NewAssetService`

## 3. 后端测试

- [x] 3.1 添加 `TestAssetService_List_IncludeHoldingsFalse_DoesNotLoadHoldings`（不传 include_holdings 时 HoldingSummary 应为 nil）
- [x] 3.2 添加 `TestAssetService_List_IncludeHoldingsTrue_WithoutHoldingSvc_DoesNotLoad`（IncludeHoldings=true 但 holdingSvc=nil 时不加载，不 panic）
- [x] 3.3 添加 `TestAssetService_List_IncludeHoldingsTrue_WithHoldingSvc_LoadsHoldings`（应正确加载 HoldingSummary）

## 4. 前端 API 与类型

- [x] 4.1 在 `frontend/src/api/asset.ts` 的 List 参数类型中加入可选 `include_holdings?: boolean`
- [x] 4.2 在 `frontend/src/api/types.ts` 中定义 `AssetHoldingSummary` 接口（与后端 `domain.HoldingSummary` 对齐）
- [x] 4.3 在 `Asset` 类型上添加 `holding_summary?: AssetHoldingSummary | null` 可选字段

## 5. 前端股票页（StockManage.vue）

- [x] 5.1 `fetchList()` 调用 `assetApi.stocks(...)` 时传 `include_holdings: true`
- [x] 5.2 实现 `getPnlColor` / `formatPnl` / `formatPnlRatio` 三个格式化函数
- [x] 5.3 表格新增列：持有数量、平均成本、市值、未实现盈亏、总盈亏、盈亏比率、已实现盈亏、累计分红
- [x] 5.4 盈亏列使用 `:style="{ color: getPnlColor(...) }"` 显示正绿负红

## 6. 前端理财页（WealthManage.vue）

- [x] 6.1 `fetchList()` 调用 `assetApi.wealth(...)` 时传 `include_holdings: true`
- [x] 6.2 实现 `getPnlColor` / `formatPnl` / `formatPnlRatio` 三个格式化函数
- [x] 6.3 表格新增列：持有份额、平均成本、最新净值、市值、未实现盈亏、总盈亏、盈亏比率、已实现盈亏、累计利息

## 7. 前端现金页（CashManage.vue）

- [x] 7.1 `fetchList()` 调用 `assetApi.cash(...)` 时传 `include_holdings: true`
- [x] 7.2 实现 `getPnlColor` / `formatPnl` / `formatPnlRatio` 三个格式化函数
- [x] 7.3 表格展示：持有金额、盈亏比率、总盈亏

## 8. 联调与验证

- [ ] 8.1 启动后端 `go run ./cmd/api`，访问 `GET /api/v1/assets?asset_type=stock`，验证不带 include_holdings 时返回不含 `holding_summary`
- [ ] 8.2 访问 `GET /api/v1/assets?asset_type=stock&include_holdings=true`，验证返回每个资产带 `holding_summary` 嵌套对象
- [ ] 8.3 启动前端 `npm run dev`，访问 `/stock` 页面，验证持仓列正确显示
- [ ] 8.4 访问 `/wealth` 页面，验证持仓与利息列正确显示
- [ ] 8.5 访问 `/cash` 页面，验证持有金额与盈亏列正确显示
- [ ] 8.6 验证无持仓资产显示 `-` 占位、不报错
- [ ] 8.7 验证盈亏颜色：正绿负红、零灰色
