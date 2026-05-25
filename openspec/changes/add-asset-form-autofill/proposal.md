## Why

新增基金 / 股票时用户必须手工录入"名称、基金公司、基金经理、基金类型、行业、板块、最新价/最新净值"等字段，对零理财经验的目标用户而言录入门槛高、错误率大。系统已经接入东方财富 / 新浪 / 腾讯行情 API，把这些公开可得的字段在录入时自动拉取，能显著降低录入摩擦，并且与产品目标"帮助无投资经验的人提升投资理财能力"保持一致。

## What Changes

- **新增**：基金录入页"基金代码"输入框旁新增 `📥 获取信息` 按钮，点击后调用后端探测接口，按"仅填空"策略回填：基金名称、基金公司、基金经理、基金类型、最新净值、净值日期。
- **新增**：股票录入页"股票代码"输入框旁新增 `📥 获取信息` 按钮，点击后按"仅填空"策略回填：股票名称、市场（由代码前缀推断 SH/SZ/BJ）、行业、板块、上市日、最新价。
- **新增**：后端新增 `GET /api/v1/assets/probe?asset_type=fund|stock&asset_code=xxx&market=SH` 接口，统一返回 `AssetProbeResult`（同时含 `name / company / manager / fund_type / industry / sector / listing_date / latest_price / latest_nav / nav_date`，无值字段省略）；接口仅供已登录用户调用，遵循现有 D13 身份注入规范。
- **扩展**：`platformapi` 包新增 `AssetMetaFetcher` 抽象（与现有 `QuoteFetcher` 平级），`EastmoneyFetcher` 实现该抽象——基金走 `https://fund.eastmoney.com/pingzhongdata/{code}.js` 解析公司/经理/类型；股票走 `https://push2.eastmoney.com/api/qt/stock/get` 增加 `f127`(行业)/`f128`(板块)/`f189`(上市日) 字段并复用现有股票端点。
- **不做**：理财产品录入页无公开数据源，本变更不引入"获取信息"按钮（保留手工录入）。
- **不破坏**：现有 `QuoteFetcher`、行情刷新、AI 把脉链路保持不变；资产表单已填字段一律保留，自动填充只覆盖空字段。

## Capabilities

### New Capabilities
- `asset-form-autofill`: 资产录入表单（基金、股票）按代码自动探测并回填可公开获取字段，统一通过后端 probe 接口，遵循"仅填空"覆盖策略。

### Modified Capabilities
<!-- 无 -->

## Impact

- **后端代码**：
  - 新增 `backend/internal/platformapi/types.go` 中 `AssetMeta` / `AssetMetaFetcher` 抽象。
  - 新增 `backend/internal/platformapi/eastmoney_meta.go`（基金详情 + 股票详情解析）。
  - 新增 `backend/internal/service/asset_probe_service.go`。
  - 新增 `backend/internal/handler/asset_probe.go`（也可合并到 `asset_handler.go`，作为 `g.GET("/probe", ...)` 子路由）。
  - 修改 `backend/internal/bootstrap/wire.go`、`backend/internal/bootstrap/handlers.go`（依赖装配）。
- **前端代码**：
  - 新增 `frontend/src/api/asset_probe.ts`。
  - 修改 `frontend/src/views/asset/FundManage.vue`、`StockManage.vue`，在代码输入框右侧新增 `📥 获取信息` 按钮 + 调用逻辑。
- **依赖**：复用现有 `resty` HTTP 客户端，无新增第三方库。
- **配置**：复用现有 `eastmoney` baseURL 配置，无需新增配置项；探测接口超时复用 `quote.timeout`。
- **测试**：新增 `eastmoney_meta_test.go` httptest 桩；新增 service / handler 单测；前端按现状不强制单测。
- **文档**：本变更归档时更新 `docs/delivery/checklist.md` 录入项验收说明。
