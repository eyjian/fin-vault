## 1. 后端 platformapi 抽象 + 东方财富元信息实现

- [x] 1.1 在 `backend/internal/platformapi/types.go` 新增 `AssetMeta` 结构体（含基金/股票字段，decimal/time 字段使用 zero value 表示缺失）和 `AssetMetaFetcher` 接口
- [x] 1.2 新增 `backend/internal/platformapi/eastmoney_meta.go`，实现 `eastmoneyMetaFetcher`：
  - 基金分支：调 `https://fund.eastmoney.com/pingzhongdata/{code}.js`，用 regex 提取 `fS_name`、`Data_currentFundManager`（取 `name` 字段）、`fund_sourceRate` 之类公司/类型字段；解析失败的非关键字段做 graceful degrade
  - 股票分支：复用现有 stock 端点，把 `fields` 扩展为 `f43,f57,f58,f127,f128,f189`，解析行业/板块/上市日，本地按代码前缀推断 `Market`
  - 不支持的 asset_type 返回 `ErrUnsupportedAsset`
- [x] 1.3 在 `backend/internal/platformapi/eastmoney_meta.go` 加 `Source() string` 返回 `"api_eastmoney"`、`Supports(AssetKey) bool` 限定 `fund`/`stock`
- [x] 1.4 提供 `WithFundDetailBaseURL` / 复用 `WithStockBaseURL` Option 以便测试桩注入
- [x] 1.5 新增 `backend/internal/platformapi/eastmoney_meta_test.go`：用 httptest 桩覆盖
  - 基金成功：name/company/manager/fund_type 全字段提取
  - 基金部分字段缺失：仅返回成功提取的字段，不整体失败
  - 股票成功：name/industry/sector/listing_date/latest_price 解析正确，市场前缀推断 `SH`/`SZ`/`BJ`
  - 错误路径：空 body → ErrNoData；asset_type=wealth → ErrUnsupportedAsset

## 2. 后端 service 层

- [x] 2.1 新增 `backend/internal/service/asset_probe_service.go`：
  - 定义 `AssetProbeService` 结构体，构造函数接收 `platformapi.AssetMetaFetcher`（接口而非具体类型，便于测试）
  - 方法 `Probe(ctx, ProbeArgs) (*AssetProbeResult, error)`：参数校验 + 调 fetcher + 错误归一化（`ErrNoData` → 上层 404 错误码、其它 → 502）
- [x] 2.2 在 `backend/pkg/errs` 复用现有错误风格，新增 `ErrAssetProbeNotFound` / `ErrAssetProbeUpstream` 哨兵错误，handler 层映射 HTTP 状态
- [x] 2.3 新增 `backend/internal/service/asset_probe_service_test.go`：mock fetcher，覆盖
  - 入参缺失 → 422 类错误
  - 远端 ErrNoData → ErrAssetProbeNotFound
  - 远端其它错误 → ErrAssetProbeUpstream
  - 成功路径 → 返回值字段映射正确

## 3. 后端 handler 层

- [x] 3.1 在 `backend/internal/handler/asset_handler.go` 中给 `g := r.Group("/assets")` 注册 `g.GET("/probe", h.Probe)`，handler 方法挂在 `AssetHandler` 上（依赖注入新增 `probeSvc *service.AssetProbeService`）；或新增 `asset_probe_handler.go` 单独承载
- [x] 3.2 实现 `Probe` 方法：解析 query 参数 → 调 service → 按 sentinel 错误映射 422/404/502 → 返回 `AssetProbeResponse{name, company?, manager?, fund_type?, industry?, sector?, listing_date?, latest_price?, latest_nav?, nav_date?, source}`，所有 optional 字段使用 `omitempty`
- [x] 3.3 新增 `backend/internal/handler/asset_probe_handler_test.go`：用 gin httptest 跑完整链路，覆盖 200/422/404/502/401（401 由 middleware.Auth 真实拦截）

## 4. 后端 bootstrap 装配

- [x] 4.1 在 `backend/internal/bootstrap/wire.go` 中：
  - 构造 `metaFetcher := platformapi.NewEastmoneyMetaFetcher(httpTimeout)`
  - 构造 `probeSvc := service.NewAssetProbeService(metaFetcher)`
  - 注入到 `AssetHandler`（或独立 handler）
- [x] 4.2 在 `backend/internal/bootstrap/handlers.go`（如有）补依赖
- [x] 4.3 启动后 `curl /api/v1/assets/probe?asset_type=fund&asset_code=110022` 自测通过（带登录 cookie）

## 5. 前端 API 封装

- [x] 5.1 新增 `frontend/src/api/asset_probe.ts`，导出 `assetProbeApi.probe(params: { asset_type, asset_code, market? })`，返回 `Promise<AssetProbeResult>`
- [x] 5.2 在 `frontend/src/api/types.ts` 中补 `AssetProbeResult` 类型（与后端 JSON 字段一一对应，全部 optional 除了 `source`）

## 6. 前端基金录入表单改造

- [x] 6.1 在 `frontend/src/views/asset/FundManage.vue` 的 `<el-form-item label="基金代码">` 旁，把 `el-input` 改成带 `<template #append>` 的形态，append 内放一个 `<el-button :icon="Download" :disabled="!form.asset_code" @click="onProbeFund">📥 获取信息</el-button>`
- [x] 6.2 实现 `onProbeFund()`：调 API、loading 态、按"仅填空"策略写入 `name / fund_detail.company / fund_detail.manager / fund_detail.fund_type / fund_detail.latest_nav / fund_detail.latest_nav_date`；ElMessage 成功/失败提示
- [x] 6.3 编辑模式打开时（`openEdit`）按钮仍可见但语义保持一致（按钮始终是"按代码探测并仅填空"，编辑模式下若所有字段已填则等于 no-op）

## 7. 前端股票录入表单改造

- [x] 7.1 在 `frontend/src/views/asset/StockManage.vue` 的"股票代码"输入框右侧新增 `📥 获取信息` 按钮，disabled 条件：`!form.asset_code || !form.stock_detail?.market`
- [x] 7.2 在 `asset_code` 变更时触发"按前缀建议 market"逻辑（仅在 `market` 为空或与建议值一致时建议；用户已选则不动）
- [x] 7.3 实现 `onProbeStock()`：调 API、按"仅填空"策略写入 `name / stock_detail.industry / stock_detail.sector / stock_detail.listing_date / stock_detail.latest_price`；保留用户已选 `market`

## 8. 前端理财页验证（保持原状）

- [x] 8.1 检查 `frontend/src/views/asset/WealthManage.vue` 录入表单**未**新增任何探测按钮
- [x] 8.2 在 PR 描述/checklist 中显式标注"理财不支持自动填充"

## 9. 联调与回归

- [x] 9.1 后端 `go test ./internal/platformapi/... ./internal/service/... ./internal/handler/...` 全绿
- [x] 9.2 前端 `npm run build` 通过；手工跑通：新增基金（110022）、新增股票（600519/00700）两条主路径
- [x] 9.3 验证 spec 中的所有 Scenario：
  - 基金成功 / 股票 A 股成功 / 远端无数据 / 远端 5xx / 未登录 / 缺参 / 已填字段不被覆盖 / 失败不阻塞 / 理财页无按钮
- [x] 9.4 `openspec validate add-asset-form-autofill --strict` 通过
- [x] 9.5 在 `docs/delivery/checklist.md` 中"基金/股票录入"行追加自动填充验收点

## 10. 港股探测失败修复（Referer + 多源降级）

- [x] 10.1 为 `eastmoneyMetaFetcher` 的 resty client 添加 `Referer: https://quote.eastmoney.com` 头，解决东方财富 API 可能因缺少 Referer 而拒绝请求的问题
- [x] 10.2 为 `eastmoneyFetcher`（行情 Fetcher）同步添加 `Referer: https://quote.eastmoney.com` 头
- [x] 10.3 增大东方财富 meta fetcher 的重试次数（1→2）和重试间隔（500ms→800ms），提升不稳定网络下的成功率
- [x] 10.4 新增 `backend/internal/platformapi/sina_meta.go`，实现 `sinaMetaFetcher` 作为备用元信息源（支持港股/A股，可返回名称+当前价）
- [x] 10.5 修改 `AssetProbeService` 支持多源降级：构造函数改为可变参数 `NewAssetProbeService(fetchers ...platformapi.AssetMetaFetcher)`，Probe 方法按优先级顺序尝试，第一个成功即返回
- [x] 10.6 修改 `wire.go` 装配：注入 `sinaMetaFetcher` 作为第二优先级备用源
- [x] 10.7 新增多源降级测试用例：主源失败备用源成功 / 全部失败 / 全部 NoData / 不支持的资产类型跳过
- [x] 10.8 更新 service 注释，反映多源降级设计
