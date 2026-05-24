## MODIFIED Requirements

### Requirement: 内置工具集合

系统 SHALL 至少提供以下内置工具：`search_fund`、`market_quote`、`history_query`、`holding_query`、`profit_calc`、`platform_summary`、`pulse_diagnosis`。

#### Scenario: pulse_diagnosis 对单资产把脉

- **WHEN** 模型调用 `pulse_diagnosis(asset_id=N)`
- **THEN** 工具 SHALL 调用 `PulseDiagnosisService.Diagnose(ctx, userID, assetID=N, triggerSource="chat")`
- **AND** 返回包含 `recommendation`（sell/reduce/hold/add）、`confidence`（high/medium/low）、`summary`（简要原因≤0 字）、`detail`（详细分析含投资知识解释）
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
