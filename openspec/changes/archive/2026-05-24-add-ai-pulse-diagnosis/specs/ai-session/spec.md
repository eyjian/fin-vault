## MODIFIED Requirements

### Requirement: 把脉结果关联 AI 会话消息

系统 SHALL 在执行把脉时，把脉过程作为 assistant 消息记录到 AI 会话中，包含把脉结论和分析原因。

#### Scenario: 把脉结果写入 AI 会话

- **WHEN** 用户通过资产管理页面或 AI 对话触发一次把脉
- **THEN** 系统 SHALL 在对应的 AI 会话中写入一条 assistant 消息，格式化展示把脉结论（如"建议减仓"）和分析原因
- **AND** `t_fv_ai_pulse_diagnoses` 表的 `f_session_id` 字段关联到该会话

#### Scenario: 从 AI 对话中查看把脉历史

- **WHEN** 用户打开某个 AI 会话的历史消息
- **THEN** 消息列表中 SHALL 包含之前的把脉结果（作为 assistant 消息的一部分）
