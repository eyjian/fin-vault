## Context

当前 `/stock`、`/wealth`、`/cash`、`/fund` 等资产列表页面只展示资产基本信息（代码、名称、市场等），缺少持仓和盈亏数据。前端要展示持仓数据有两种思路：

1. **二次请求**：列表页拉到资产后，再为每个资产调一次 `/holdings/summary?asset_id=...`，N+1 调用、性能差。
2. **服务端聚合**：在 Asset List 接口内可选地预加载持仓数据，一次性返回。

后端已经存在 `HoldingService.GetSummaryByAsset(userID, assetID)` 接口，能够计算单资产的持仓汇总（数量、成本、市值、盈亏、盈亏比率等）。Asset Service 与 Holding Service 同属于 service 层，互相调用合规。

约束：
- API 兼容性：现有调用方不传 `include_holdings` 参数时行为不变。
- 性能：`include_holdings=true` 时每个资产需查询一次持仓汇总，单页 ≤20 条数据时延迟可接受。
- 金额精度：持仓字段使用 `decimal.Decimal`，前端字符串传输保精度。

## Goals / Non-Goals

**Goals:**
- 在 `GET /assets` 接口提供可选 `include_holdings=true` 参数，开启后每条资产返回 `holding_summary` 对象。
- 前端 `/stock`、`/wealth`、`/cash` 页面在表格中直接展示持仓数量、平均成本、市值、未实现盈亏、总盈亏、盈亏比率、已实现盈亏、累计分红/利息等列。
- 无持仓资产返回 `holding_summary: null`，前端显示 `-` 占位。

**Non-Goals:**
- 不引入新的 API 端点（仅在 `GET /assets` 上加参数）。
- 不重写 Holding Service 的汇总逻辑，复用 `GetSummaryByAsset`。
- 不修改单个 Holding 的查询接口（`/holdings`）。
- 不优化 N 次查询为 IN 批量（一期接受 O(N) 次 GetSummaryByAsset，单页 ≤ 20 性能可接受）。
- `/fund` 页面的持仓列由独立变更 `add-holding-summary-to-fund` 处理。

## Decisions

### Decision 1：在 Asset domain 上以 `gorm:"-"` 字段携带 HoldingSummary

```go
type Asset struct {
    // ... 基本字段
    HoldingSummary *HoldingSummary `gorm:"-" json:"holding_summary,omitempty"`
}
```

**为什么**：避免新增 DTO 类型层，复用 domain.Asset 直接序列化。`gorm:"-"` 保证不污染数据库映射；`omitempty` 保证不开启时 JSON 输出干净。

**备选方案**：单独定义 `AssetWithHoldingDTO`。被否决，因为会带来 domain ↔ DTO 转换样板代码；当前阶段 domain 直出 JSON 是项目惯例。

### Decision 2：在 Asset Service.List 入参增加 `IncludeHoldings bool` 开关

```go
type AssetListInput struct {
    // ... 既有字段
    IncludeHoldings bool
}
```

Handler 解析 `c.Query("include_holdings") == "true"` 写入 input。Service 内若 `IncludeHoldings && holdingSvc != nil` 则对结果遍历调用 `holdingSvc.GetSummaryByAsset` 注入 `HoldingSummary`。

**为什么**：把"是否聚合持仓"作为业务参数下沉到 service，handler 只做协议转换，与既有五层架构一致。

### Decision 3：Asset Service 通过构造函数注入 HoldingService（可空）

```go
func NewAssetService(repo, ..., holdingSvc *HoldingService) *AssetService
```

`holdingSvc` 允许为 `nil`，便于测试时不构建持仓依赖。

**为什么**：避免循环依赖（HoldingService 不依赖 AssetService.List），依赖方向单一。

**备选方案**：在 service 内 new 一个 HoldingService。被否决，违反 wire 装配规范。

### Decision 4：持仓字段用嵌套对象 `holding_summary` 而非平铺字段

JSON 输出形如：
```json
{
  "id": 1, "asset_code": "600519", "name": "贵州茅台",
  "holding_summary": {
    "quantity": "100", "avg_cost": "1500.00",
    "market_value": "180000.00", "unrealized_pnl": "30000.00",
    "total_pnl": "32000.00", "pnl_ratio": "0.2133",
    "realized_pnl": "0", "total_dividend": "2000.00",
    "latest_price": "1800.00", "total_cost": "150000.00"
  }
}
```

**为什么**：与 spec 中描述的字段名（`holding_quantity` 等平铺方案）相比，嵌套结构更清晰、易扩展，且与前端 `AssetHoldingSummary` 类型一一对应。spec 中字段名已在实施过程中调整为 `holding_summary.*` 形式（前端 `row.holding_summary?.quantity` 等）。

### Decision 5：盈亏颜色与格式化在前端三个页面各自实现

每个 Vue 页面里复制三个小函数：`getPnlColor`、`formatPnl`、`formatPnlRatio`。

**为什么**：当前三个页面页面形态接近但不完全相同（Wealth 多"最新净值"列、Cash 列更精简），抽公共组件收益小、风险大。后续若再加资产类型可考虑提取到 `frontend/src/utils/pnl.ts`。

## Risks / Trade-offs

- **N 次持仓查询的性能** → 缓解：单页 ≤ 20，每次 `GetSummaryByAsset` 走主键索引；后续若数据量上升再批量化（IN 查询 + 内存聚合）。
- **持仓字段为 null 时前端展示** → 缓解：模板统一用 `row.holding_summary?.quantity || '-'`，无持仓资产返回 `holding_summary: null`，前端显示 `-`。
- **金额字符串精度** → 缓解：后端 `decimal.Decimal` 序列化为字符串，前端 `parseFloat` 仅用于颜色判断与展示格式化，不参与计算。
- **API 兼容性** → 缓解：`include_holdings` 是可选 query 参数，旧调用方不传时行为不变。
- **测试断言字段命名漂移**：实施时把 spec 里的 `holding_quantity` 等平铺字段改为 `holding_summary.*` 嵌套形式 → 缓解：spec 在归档前同步调整字段命名，或在归档时记录此次修订。

## Migration Plan

无数据库迁移。仅需：
1. 后端编译并重启服务（向后兼容）。
2. 前端构建发布，旧版本前端不开 `include_holdings` 也能正常工作。

回滚策略：移除前端对 `include_holdings` 的传递即可回到旧行为，后端无需回滚。

## Open Questions

- 是否需要把 `holding_summary` 的 PnL 字段全部转换到 `display_currency`？当前实现按资产币种返回，跨币种汇总在 Dashboard 页用 `HoldingService.Summary` 聚合，列表页保持原币种符合用户预期。如果未来支持多币种合并视图再扩展。
