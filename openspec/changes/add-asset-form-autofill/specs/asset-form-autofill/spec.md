## ADDED Requirements

### Requirement: 探测接口按代码返回资产可公开元信息

系统 SHALL 提供 `GET /api/v1/assets/probe` 接口，根据 `asset_type` + `asset_code`（股票还需 `market`）从公开数据源探测可获取的资产元信息，并以 JSON 形式返回；接口 MUST 经过登录鉴权，无值字段在响应中省略。

#### Scenario: 基金代码探测成功

- **WHEN** 已登录用户调用 `GET /api/v1/assets/probe?asset_type=fund&asset_code=110022`，且远端返回完整基金详情
- **THEN** 接口返回 200，body 包含 `name`、`company`、`manager`、`fund_type`、`latest_nav`、`nav_date`、`source` 字段；`source` 取值形如 `api_eastmoney`

#### Scenario: A 股股票代码探测成功

- **WHEN** 已登录用户调用 `GET /api/v1/assets/probe?asset_type=stock&asset_code=600519&market=SH`
- **THEN** 接口返回 200，body 至少包含 `name`、`market=SH`、`latest_price`、`source`；A 股 SHALL 同时尝试返回 `industry` 和 `sector`，端口未返回的字段省略不报错

#### Scenario: 远端无该代码对应数据

- **WHEN** 用户调用探测接口，但远端公开数据源对该代码返回空响应
- **THEN** 接口返回 404，body 形如 `{"code":"PROBE_NOT_FOUND","message":"未找到该代码对应的公开信息"}`

#### Scenario: 远端 HTTP 调用失败

- **WHEN** 探测过程中远端 API 超时、网络错误或解析失败
- **THEN** 接口返回 502，body 形如 `{"code":"PROBE_UPSTREAM_ERROR","message":"<具体错误描述>"}`

#### Scenario: 未登录用户调用被拒绝

- **WHEN** 未登录用户调用 `GET /api/v1/assets/probe?...`
- **THEN** 接口返回 401（由全局 `middleware.Auth` 拦截），不进入业务逻辑

#### Scenario: 缺失或非法参数

- **WHEN** 调用时缺失 `asset_code`，或 `asset_type` 不在 `{fund, stock}` 内，或 `asset_type=stock` 时缺失 `market`
- **THEN** 接口返回 422，body 含具体的校验失败说明

### Requirement: 探测能力作为独立 fetcher 抽象与行情解耦

系统 SHALL 在 `platformapi` 包中提供与 `QuoteFetcher` 平级的 `AssetMetaFetcher` 接口，并由 `EastmoneyMetaFetcher` 实现；该抽象 MUST 不被 `QuoteAggregator` 与 `QuoteService.RefreshLatest` 依赖，行情刷新链路保持原有性能与字段不变。

#### Scenario: 探测接口失败不影响行情刷新

- **WHEN** `EastmoneyMetaFetcher` 因远端故障返回错误
- **THEN** `QuoteAggregator` 与 `QuoteService.RefreshLatest` 工作不受任何影响（依赖独立、调用栈不交叉）

#### Scenario: AssetMetaFetcher 按 asset_type 路由

- **WHEN** 调用 `EastmoneyMetaFetcher.FetchMeta(ctx, AssetKey{AssetType: "fund", AssetCode: "110022"})`
- **THEN** 实现内部 SHALL 走基金详情端点（如 `pingzhongdata`），返回 `AssetMeta{Name, Company, Manager, FundType, LatestNAV, NAVDate, Source}`

#### Scenario: AssetMetaFetcher 不支持的 asset_type 直接报错

- **WHEN** 调用 `FetchMeta` 时 `AssetType` 为 `wealth` 或 `cash`
- **THEN** 返回 `platformapi.ErrUnsupportedAsset`，不发起任何 HTTP 调用

### Requirement: A 股市场可由代码前缀本地推断

当 `asset_type=stock` 且代码为 6 位 A 股代码时，系统 SHALL 在前端按代码前缀规则推断 `market`：`6` 开头映射 SH，`0` / `3` 开头映射 SZ，`8` / `4` 开头映射 BJ；推断结果 MUST 在用户已选择 `market` 时不覆盖用户的选择。

#### Scenario: 输入 600519 自动建议 SH

- **WHEN** 用户在新增股票表单中输入 `asset_code=600519`、且尚未选择 `market`
- **THEN** 表单 SHALL 把 `market` 自动建议为 `SH`，并允许用户随时改回其它值

#### Scenario: 用户已选 HK，再输入代码不被覆盖

- **WHEN** 用户先选择了 `market=HK`，再输入 `asset_code=00700`
- **THEN** `market` 保持 `HK` 不变，前端不进行任何前缀推断

### Requirement: 前端代码框旁提供"获取信息"按钮，仅填空字段

新增基金、新增股票表单 SHALL 在"代码"输入框右侧提供 `📥 获取信息` 按钮；按钮在 `asset_code` 为空、或 `asset_type=stock` 且 `market` 未选时 disabled；点击后调用 `/api/v1/assets/probe`，并仅把表单中"为空"的字段（含空字符串、null、undefined）写入返回的对应字段，**已填字段一律保留**。

#### Scenario: 仅填空字段，名称已填则不被覆盖

- **WHEN** 用户已经手填 `name="我自己的备注名"`，然后点击 `📥 获取信息` 且远端返回 `name="易方达消费行业"`
- **THEN** 表单中的 `name` 保持 `"我自己的备注名"` 不变；其他空字段（如 `company`、`manager`）按返回值填充

#### Scenario: 探测失败提示但不阻塞录入

- **WHEN** 用户点击按钮后 API 返回 404 或 502
- **THEN** 前端 SHALL 用 `ElMessage.warning` 弹出失败原因（如"未找到该代码对应的公开信息"），表单字段 MUST 保持点击前的值，用户能继续手工录入

#### Scenario: 探测成功提示已填字段数

- **WHEN** 探测成功，回填 N 个空字段
- **THEN** 前端 SHALL 用 `ElMessage.success` 提示形如"已自动填充 N 个字段"

#### Scenario: 按钮在前置条件不满足时禁用

- **WHEN** `asset_code` 为空，或新增股票时 `market` 未选
- **THEN** `📥 获取信息` 按钮 SHALL 处于 disabled 状态，无法点击

### Requirement: 理财产品录入页不引入自动填充按钮

理财产品（`WealthManage.vue`）录入表单 SHALL NOT 出现 `📥 获取信息` 按钮；理财探测能力本期不实现，后端 `/api/v1/assets/probe?asset_type=wealth` 调用 MUST 返回 422（asset_type 非法）。

#### Scenario: 理财录入页保持原状

- **WHEN** 用户进入新增理财产品弹窗
- **THEN** UI 上 SHALL 不出现"获取信息"按钮，所有字段全部手工录入

#### Scenario: 后端拒绝 wealth 类型

- **WHEN** 任何客户端调用 `GET /api/v1/assets/probe?asset_type=wealth&asset_code=xxx`
- **THEN** 接口 SHALL 返回 422，提示 `asset_type` 不在支持列表内
