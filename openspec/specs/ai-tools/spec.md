# ai-tools Specification

## Purpose
TBD - created by archiving change replace-ai-with-trpc-agent-go. Update Purpose after archive.
## Requirements
### Requirement: 工具统一接入 trpc-agent-go 的 Tool 接口

系统 SHALL 将所有 AI 可调用的业务工具按 `tool.Tool` 接口实现，并通过 Agent Runner 注册，禁止在 Service / Handler 层手写 `tool_calls` 解析。

#### Scenario: 工具自动注册

- **WHEN** 后端启动
- **THEN** Bootstrap SHALL 把所有内置工具注册到 Agent Runner 上，工具名采用 snake_case
- **AND** 启动日志列出已注册工具及其参数 schema

#### Scenario: 工具未注册时被拒绝

- **WHEN** 模型返回了一个未注册的工具名
- **THEN** Runner SHALL 拒绝调用并返回业务错误 `AI_TOOL_NOT_FOUND`，不发起任何外部请求

### Requirement: 内置工具集合

系统 SHALL 至少提供以下内置工具：`search_fund`、`market_quote`、`history_query`、`holding_query`、`profit_calc`、`platform_summary`、`pulse_diagnosis`。

#### Scenario: search_fund 模糊匹配

- **WHEN** 模型调用 `search_fund(keyword="医药")`
- **THEN** 工具 SHALL 在本地基金表内按 `name LIKE %医药%` 或 `code = '医药'` 检索，返回不超过 20 条结果，每条含 `code`、`name`、`type`

#### Scenario: market_quote 查上证指数

- **WHEN** 模型调用 `market_quote(symbol="sh000001")`
- **THEN** 工具 SHALL 返回 `symbol`、`name="上证指数"`、`price`、`change`、`change_percent`、`updated_at`
- **AND** 工具底层数据源失败时 SHALL 返回业务错误 `AI_TOOL_FAILED`，并附 `provider` 字段

#### Scenario: holding_query 仅看本人持仓

- **WHEN** 模型在用户 A 的会话内调用 `holding_query()`
- **THEN** 工具 SHALL 仅返回 `user_id = A` 的持仓
- **AND** 在工具调用日志中带上 `user_id=A`，便于审计

#### Scenario: pulse_diagnosis 对单资产把脉

- **WHEN** 模型调用 `pulse_diagnosis(asset_id=N)`
- **THEN** 工具 SHALL 调用 `PulseDiagnosisService.Diagnose(ctx, userID, assetID=N, triggerSource="chat")`
- **AND** 返回包含 `recommendation`（sell/reduce/hold/add）、`confidence`（high/medium/low）、`summary`（简要原因）、`detail`（详细分析含投资知识解释）
- **AND** 在工具调用日志中记录 `asset_id` 和 `user_id`，便于审计

#### Scenario: pulse_diagnosis 批量资产把脉

- **WHEN** 模型调用 `pulse_diagnosis(asset_ids=[N1,N2,...])`（数组参数）
- **THEN** 工具 SHALL 逐个资产调用 `PulseDiagnosisService.Diagnose`（Agent 工具层串行，以保证 LLM Agent 的控制流稳定）
- **AND** 返回数组结果，每个元素包含 `asset_id`、`recommendation`、`confidence`、`summary`、`detail`
- **AND** REST API 入口的批量把脉采用并行调用（详见 `ai-pulse-diagnosis` spec），两者不冲突

#### Scenario: pulse_diagnosis 身份隔离

- **WHEN** 模型在用户 A 的会话内调用 `pulse_diagnosis()`
- **THEN** 工具 SHALL 仅基于 `user_id = A` 的数据进行分析
- **AND** 入参 schema 不含 `user_id` 字段，身份从 ctx 自动注入（与 D13 规则 1 一致）

### Requirement: 工具参数 schema 由结构体生成

系统 SHALL 由工具的 Go 结构体（带 `json` 与 `description` tag）自动生成 JSON Schema，禁止手写 schema 字符串。

#### Scenario: 新增工具的成本

- **WHEN** 开发者新增一个工具
- **THEN** 仅需新增一个 `*.go` 文件（含 `Name`/`Description`/`Run` 三个方法和入参结构体），运行时 SHALL 自动生成 schema 并完成注册
- **AND** 不需要修改任何中心化的注册表代码（不存在 `tools/registry.go`）

### Requirement: 工具调用对用户透明可见

系统 SHALL 把 Agent 在一次 turn 内调用过的工具列表，作为响应的一部分返回给前端。

#### Scenario: 前端展示工具调用过程

- **WHEN** 前端 `POST /api/v1/ai/sessions/{id}/messages`
- **THEN** 响应体 SHALL 含 `tool_calls`：`[{name, arguments, started_at, finished_at, status}]` 数组
- **AND** 即使工具失败，该数组也包含失败记录，前端可展示"调用 search_fund 失败"

#### Scenario: 数据脱敏

- **WHEN** 工具入参或返回值包含 `api_key` / `password` / `token` 字段
- **THEN** 系统 SHALL 在落库与返回前掩码为 `***`，不暴露给前端
