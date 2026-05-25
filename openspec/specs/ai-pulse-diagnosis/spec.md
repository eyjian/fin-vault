## ADDED Requirements

### Requirement: AI 把脉功能

系统 SHALL 提供 AI 把脉功能，对用户持有的基金、股票、理财产品进行综合分析，给出操作建议、置信度及分层原因（简要 + 详细），帮助无理财经验的用户进行投资决策并学习投资知识。

#### Scenario: 单资产把脉

- **WHEN** 用户在资产管理页面点击某个资产的"AI 把脉"按钮
- **THEN** 系统 SHALL 调用 AI 分析该资产的持仓数据、行情数据、盈亏数据，返回把脉结果
- **AND** 把脉结果包含四类之一：`sell`（建议卖出）、`hold`（继续持有）、`add`（建议加仓）、`reduce`（建议减仓）
- **AND** 包含置信度 `confidence`：`high` / `medium` / `low`
- **AND** 包含简要原因 `summary`（≤80 字，引用具体数据）和详细原因 `detail`（≥150 字，含投资知识解释）
- **AND** 结果持久化到 `t_fv_ai_pulse_diagnoses` 表，下次打开直接显示

#### Scenario: 批量把脉（并行化）

- **WHEN** 用户在资产管理页面选中多个资产，点击"批量 AI 把脉"按钮
- **THEN** 系统 SHALL 并行调用 `PulseDiagnosisService.Diagnose`（默认并发度 3，可配置）
- **AND** 单个资产把脉失败不阻塞其他资产，最终返回每个资产的成功/失败状态
- **AND** 所有成功的把脉结果持久化，列表中对应行的把脉结论列、置信度、原因列同步更新

#### Scenario: 把脉结果缓存

- **WHEN** 用户打开资产管理页面，且某资产已有历史把脉结果
- **THEN** 系统 SHALL 直接从数据库读取上次把脉结果展示，不触发新把脉
- **AND** 用户可手动点击"重新把脉"来刷新结果

#### Scenario: 数据不足时的把脉

- **WHEN** 对一个资产执行 AI 把脉，但该资产没有持仓数据或行情数据
- **THEN** 系统 SHALL 返回 `recommendation=hold`、`confidence=low`、`summary="数据不足，建议补全后再把脉"`
- **AND** 不勉强给出加仓/减仓/卖出建议

#### Scenario: 分层原因展示

- **WHEN** 把脉完成后
- **THEN** 列表页在对应资产行新增"把脉结论"列、"置信度"标记、"简要原因"列
- **AND** "把脉结论"用彩色 `el-tag` 显示（sell→红/danger、reduce→橙/warning、hold→绿/success、add→蓝/primary）
- **AND** 置信度为 `low` 时 UI 有视觉提示（如灰色标签或感叹号图标，提示"请谨慎参考"）
- **AND** "简要原因"列显示 `summary`（≤80 字，引用具体数据）
- **AND** 用户点击"展开"可查看 `detail`（含投资知识解释，初学者友好）

#### Scenario: 风险提示

- **WHEN** 用户查看任何把脉结果
- **THEN** 页面 SHALL 显示风险提示文案："AI 建议仅供参考，投资决策请自行判断，过往业绩不代表未来表现"

### Requirement: 把脉结果持久化

系统 SHALL 将每次把脉结果存入 `t_fv_ai_pulse_diagnoses` 表，每个用户的每个资产只保留最新一条。

#### Scenario: 把脉结果入库

- **WHEN** 一次把脉完成
- **THEN** 系统 SHALL 写入/更新 `t_fv_ai_pulse_diagnoses` 中 `(f_user_id, f_asset_id)` 对应的记录
- **AND** 字段包含 `f_recommendation`（sell/reduce/hold/add）、`f_confidence`（high/medium/low）、`f_summary`、`f_detail`、`f_data_references`（JSON）、`f_raw_response`、`f_session_id`、`f_trigger_source`（manual/chat/scheduled）、`f_created_at`/`f_updated_at`

#### Scenario: 重新把脉覆盖旧结果

- **WHEN** 对同一资产再次执行把脉
- **THEN** 系统 SHALL 覆盖更新该资产的把脉记录（`f_updated_at` 更新为当前时间，`f_created_at` 保持首次创建时间）

#### Scenario: 把脉结果可追溯

- **WHEN** 查询某资产的把脉结果
- **THEN** 系统 SHALL 返回 `diagnosed_at`（最新把脉时间）和 `trigger_source`（触发方式：手动/对话/定时）
- **AND** 前端可基于 `diagnosed_at` 提示用户"上次把脉于 X 天前，是否需要重新把脉"

### Requirement: 把脉逻辑可复用

系统 SHALL 将把脉核心逻辑封装为 `PulseDiagnosisService.Diagnose(ctx, userID, assetID, triggerSource)` 方法，可被多个入口调用。

#### Scenario: Agent 工具调用把脉

- **WHEN** AI Agent 在对话中调用 `pulse_diagnosis` 工具
- **THEN** 工具内部 SHALL 调用 `PulseDiagnosisService.Diagnose`（triggerSource=`chat`），结果作为工具返回值传递给 Agent

#### Scenario: REST API 直调把脉

- **WHEN** 前端直接调用 `POST /api/v1/ai/pulse-diagnosis` 接口
- **THEN** handler 层 SHALL 调用 `PulseDiagnosisService.Diagnose`（triggerSource=`manual`），结果作为 HTTP 响应返回

#### Scenario: 定时把脉调用（未来扩展）

- **WHEN** 系统定时任务触发把脉（尚未实现）
- **THEN** 定时任务 SHALL 调用 `PulseDiagnosisService.Diagnose`（triggerSource=`scheduled`），逻辑与手动触发完全一致
