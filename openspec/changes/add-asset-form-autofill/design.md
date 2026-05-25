## Context

FinVault 已经在 `backend/internal/platformapi/` 下接入了东方财富 / 新浪 / 腾讯三家行情源，通过 `QuoteFetcher` 接口被 `QuoteAggregator` 聚合后供 `QuoteService` 用于"主动刷新行情"链路。现有 fetcher 仅返回 `QuoteResult{Price, ChangePct, Volume, QuoteTime, Source}`，**不包含**资产元信息（基金公司、基金经理、行业、板块等）。

资产录入页（`FundManage.vue` / `StockManage.vue` / `WealthManage.vue`）的"新增"按钮目前打开一个弹窗，所有字段均需手工录入，对零经验目标用户而言是显著的门槛（参考产品目标：帮助无投资经验的人提升投资理财能力）。

理财产品代码（如 `LC202604001`）属于机构内部编号，无统一公开 API，本次明确不做。

## Goals / Non-Goals

**Goals:**
- 在新增基金 / 新增股票表单中，用户输入"代码"后**点击按钮**即可一次性把"可公开获取的字段"回填到表单。
- 仅填空字段：用户已经手填的内容**绝不被覆盖**，避免误伤。
- 数据源失败要有清晰的用户提示（"未找到该代码对应的公开信息"），不能阻塞表单录入流程。
- 后端新增能力以**新接口**形式暴露，与现有 `QuoteService` / 行情刷新链路解耦，互不影响。
- 实现要可复用：未来如果需要"资产校验"、"代码合法性检查"等场景，能直接复用同一探测接口。

**Non-Goals:**
- 不做理财产品的自动填充（无公开数据源）。
- 不做"输入代码自动失焦触发"，必须用户主动点击按钮（避免误触发 + 节省外部 API 调用）。
- 不做美股 / 港股的 `industry`/`sector` 探测（东方财富 `f127`/`f128` 在境外股票上不稳定，仅 A 股保证可用）；港美股仅回填名称 + 最新价。
- 不引入数据库迁移，探测结果**不**入库。

## Decisions

### D1: 探测能力作为新抽象 `AssetMetaFetcher`，与 `QuoteFetcher` 平级

**选项**：
- A. 把元信息字段塞进 `QuoteResult`，复用 `QuoteFetcher`。
- B. 新增独立接口 `AssetMetaFetcher`，单独走一条链路（采用）。

**选择 B**，理由：
- `QuoteFetcher` 当前被 `QuoteService.RefreshLatest` 高频调用（资产刷新），混入元信息字段会让批量刷新做无用功（拉解析量增加几倍）。
- 元信息探测是"低频、用户触发"场景，与"高频、批量"的行情刷新职责不同；两条独立链路职责清晰。
- 抽象签名：

  ```go
  type AssetMeta struct {
      Name         string
      // fund-only
      Company      string
      Manager      string
      FundType     string  // equity/bond/hybrid/money/index/qdii
      LatestNAV    decimal.Decimal
      NAVDate      time.Time
      // stock-only
      Market       string  // 推断结果：SH/SZ/BJ
      Industry     string
      Sector       string
      ListingDate  time.Time
      LatestPrice  decimal.Decimal
      Source       string
  }

  type AssetMetaFetcher interface {
      Source() string
      Supports(a AssetKey) bool
      FetchMeta(ctx context.Context, a AssetKey) (*AssetMeta, error)
  }
  ```

### D2: 复用 `EastmoneyFetcher` 实例 vs 新建 `EastmoneyMetaFetcher`

**选项**：
- A. 在现有 `eastmoneyFetcher` 类型上加 `FetchMeta` 方法。
- B. 新建独立类型 `eastmoneyMetaFetcher`，独立 baseURL 配置（采用）。

**选择 B**，理由：
- 元信息端点（`https://fund.eastmoney.com/pingzhongdata/{code}.js` 和股票详情）与行情端点 baseURL 不同，混在一个结构体里要塞 4 个 baseURL，可读性差。
- 测试桩独立：`fetcher_test.go` 已经把行情桩做完了，元信息桩独立不污染既有测试。

### D3: 端点选型

| 类型 | 端点 | 说明 |
|---|---|---|
| 基金详情 | `https://fund.eastmoney.com/pingzhongdata/{code}.js` | JS 脚本，含 `var fS_name="..."`、`var fS_code`、`var fund_Rate`、`Data_currentFundManager` 等变量；用正则提取（不做完整 JS 解析）。 |
| 基金类型回退 | 同上 `var fund_Rate` / `Data_fundSharesPositions` 推断 | 非必需字段，解析失败 → 跳过该字段，不算整体失败 |
| 股票元信息 | `https://push2.eastmoney.com/api/qt/stock/get?secid={prefix}.{code}&fields=f57,f58,f127,f128,f43,f189` | 复用现有 `stockBaseURL`，仅扩展 `fields`：`f57`(代码) `f58`(名称) `f127`(行业) `f128`(板块) `f43`(当前价/分) `f189`(上市日 YYYYMMDD)。 |
| 市场推断 | A 股代码前缀本地推断 | `6xxxxx`→`SH`、`0xxxxx`/`3xxxxx`→`SZ`、`8xxxxx`/`4xxxxx`→`BJ`；港美股要求用户先选市场再点按钮（按钮在未选 market 时禁用）。 |

### D4: 接口形态——`GET /api/v1/assets/probe`

**选项**：
- A. 在 `AssetService` 上加方法 `Probe`，复用 `/assets` 路由。
- B. 新增 `AssetProbeService` + handler `GET /api/v1/assets/probe`（采用）。

**选择 B**，理由：
- `AssetService` 当前职责是 CRUD + 与 `HoldingService` 交互；探测属于"网络探测"职责，与 CRUD 解耦。
- 路径 `/assets/probe` 同时挂在现有 `/assets` group 下（`g.GET("/probe", h.Probe)`），保持 RESTful 资源族归类；handler 文件可独立 `asset_probe_handler.go`，也可合并到 `asset_handler.go`，由实施阶段权衡。

接口约定：
- 入参：`asset_type`（必填，`fund`/`stock`），`asset_code`（必填，最长 32 字符），`market`（股票必填）。
- 出参：`AssetProbeResponse{ name, company, manager, fund_type, industry, sector, listing_date, latest_price, latest_nav, nav_date, source }`，无值字段使用 `omitempty` 省略。
- 错误：
  - 422：参数缺失/非法。
  - 404：远端无数据（`platformapi.ErrNoData`），返回 `{"code": "PROBE_NOT_FOUND", "message": "未找到该代码对应的公开信息"}`。
  - 502：远端 HTTP 失败（超时、网络、解析），返回 `{"code": "PROBE_UPSTREAM_ERROR", ...}`。
- 鉴权：经过 `middleware.Auth`，但**不**做用户隔离（探测的是公开数据），仅要求登录态防止匿名滥用外部 API。

### D5: 前端"仅填空"覆盖策略

UI 行为：
- 代码输入框右侧 button：`📥 获取信息`，按钮在 `asset_code` 为空时 disabled。
- 点击后 loading 态（按钮禁用 + 转圈），调用 `assetProbeApi.probe(...)`。
- 收到结果后，对每个字段执行 `if (!form.xxx) form.xxx = result.xxx`（数值字段同样判断 `'' / null / undefined`）。
- 成功提示：`ElMessage.success('已自动填充 N 个字段')`，N=实际写入字段数。
- 失败提示：`ElMessage.warning(err.message)`，表单不重置、不阻塞用户继续录入。

### D6: 缓存策略

- 本期不加缓存：探测频次极低（用户每次录入资产才点 1 次），且数据时效性敏感（基金净值会变）。
- 后端复用 `EastmoneyFetcher` 的 5s timeout 默认值；外部 API 失败不重试（resty 的 `RetryCount` 已经默认 1 次）。

### D7: 测试策略

- **`backend/internal/platformapi/eastmoney_meta_test.go`**：用 `httptest.NewServer` 桩 `pingzhongdata` 与 `stock/get` 端点，验证字段提取正确性 + 错误路径（空 body/非法字段/不支持的 asset_type）。
- **`backend/internal/service/asset_probe_service_test.go`**：mock `AssetMetaFetcher`，验证 `Probe(ctx, args)` 的入参校验（asset_type、asset_code、market）+ 错误映射（ErrNoData → 404 类型错误）。
- **`backend/internal/handler/asset_probe_handler_test.go`**：用 `httptest` + gin 验证完整 HTTP 链路。
- 前端不强制单测，但要保证 `npm run build` 通过。

## Risks / Trade-offs

- [外部 API 不稳定/反爬限流] → 后端记录调用日志便于排查；前端失败提示明确告知用户"可手工填写"；后续需要时再加 in-memory cache（key=asset_type:code）。
- [`pingzhongdata` 是 JS 脚本不是 JSON] → 用 regex 提取 `var fS_name="..."` 等，对解析失败的非关键字段（如 `manager`）做 graceful degrade，仅返回成功提取的部分。
- [港美股 `f127`/`f128` 不稳定] → 仅 A 股（SH/SZ/BJ）启用 industry/sector 字段；港美股回退到只回填 name + latest_price。
- [`market` 推断错误] → A 股前缀映射有公开规则可遵循；用户在表单上仍能手动改 `market`，前端"仅填空"策略保证不覆盖用户的选择。
- [滥用外部 API] → 仅登录用户可调用；可在后续加 per-user rate limit（本期不做，留作 Open Question）。

## Migration Plan

- **部署**：纯增量改动，新接口 + 新前端按钮，与现有功能正交，可灰度发布。
- **回滚**：前端按钮可通过 feature flag（暂不实现）控制；后端接口未被任何现有流程依赖，下线 = 删除路由。
- **数据迁移**：无。

## Open Questions

- 是否需要 in-memory cache（同一资产代码 5 分钟内复用结果）？— 暂不做，待用量数据观察。
- 是否需要按用户限速？— 暂不做，等出现滥用迹象再加。
- 是否要把"探测来源"展示给用户（"来自东方财富"角标）？— 暂不展示，等收到用户反馈再决定。
