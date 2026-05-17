## ADDED Requirements

### Requirement: 创建会话

系统 SHALL 提供创建 AI 会话的能力，每个会话拥有唯一 ID、归属用户、创建时间、可选标题。

#### Scenario: 用户新建空会话

- **WHEN** 已认证用户调用 `POST /api/v1/ai/sessions`，请求体可为空
- **THEN** 系统返回 `201 Created`，响应体包含 `session_id`（UUID 字符串，长度 36）、`title`（缺省为空字符串）、`created_at`
- **AND** 数据库表 `t_fv_ai_sessions` 新增一行，`f_user_id` = 当前用户 ID

#### Scenario: 未登录用户被拒绝

- **WHEN** 未登录用户调用 `POST /api/v1/ai/sessions`
- **THEN** 系统返回 `401 Unauthorized`
- **AND** 不创建任何记录

### Requirement: 列出会话

系统 SHALL 允许用户列出自己的全部会话，按 `f_updated_at` 倒序，支持分页。

#### Scenario: 列表只返回当前用户的会话

- **WHEN** 用户 A 调用 `GET /api/v1/ai/sessions?page=1&page_size=20`
- **THEN** 响应只包含 `f_user_id` = A 的会话
- **AND** 不出现其他用户的任何记录

#### Scenario: 分页生效

- **WHEN** 用户拥有 25 条会话且请求 `page=2&page_size=20`
- **THEN** 响应返回剩余 5 条，并附带 `total=25`

### Requirement: 删除会话

系统 SHALL 允许用户删除自己的会话，删除时级联清理 `t_fv_ai_messages` 与 `t_fv_ai_agent_steps`。

#### Scenario: 删除自有会话

- **WHEN** 用户调用 `DELETE /api/v1/ai/sessions/{id}` 且会话归属本人
- **THEN** 系统返回 `204 No Content`
- **AND** `t_fv_ai_sessions` / `t_fv_ai_messages` / `t_fv_ai_agent_steps` 中所有 `f_session_id = id` 的行均被删除

#### Scenario: 拒绝删除他人会话

- **WHEN** 用户尝试删除非本人会话
- **THEN** 系统返回 `404 Not Found`，不暴露会话存在性

### Requirement: 在会话内追加消息并获取回复

系统 SHALL 允许用户向指定会话追加一条用户消息，并由 Agent 运行时给出助手回复，整轮对话作为同一 session 上下文持久化。

#### Scenario: 多轮对话保持上下文

- **WHEN** 用户在会话 S 内先后发送 "我有哪些基金？" 和 "其中哪只收益最高？"
- **THEN** 第二次请求时 Agent 必须能基于第一轮结果作答，无需重复说明"哪些基金"
- **AND** `t_fv_ai_messages` 表新增 4 行（2 条 user + 2 条 assistant），均带 `f_session_id = S`

#### Scenario: 历史窗口生效

- **WHEN** 会话 S 已累计 100 条消息且配置 `ai.session.history_window = 20`
- **THEN** 提交给 Agent 的上下文 SHALL 仅包含最近 20 条消息（含本次 user 消息）
- **AND** 早于窗口的消息保留在数据库中可被查询，但不送给模型

### Requirement: 拉取会话历史消息

系统 SHALL 允许用户拉取指定会话的全部消息，按 `f_created_at` 升序。

#### Scenario: 时间线展示

- **WHEN** 用户调用 `GET /api/v1/ai/sessions/{id}/messages`
- **THEN** 响应按时间升序返回所有 user/assistant 消息，每条包含 `role`、`content`、`created_at`、`token_usage`（可空）
- **AND** 不返回 tool 中间消息（仅作为附属事件由 step 接口提供）

### Requirement: 滚动清理超额会话步骤

系统 SHALL 按配置 `ai.session.max_steps_size_mb` 监控 `t_fv_ai_agent_steps` 表占用空间，超过阈值时按 `f_created_at` 升序删除最旧记录直至低于阈值。

#### Scenario: 阈值生效

- **WHEN** `max_steps_size_mb = 100` 且当前 `t_fv_ai_agent_steps` 占用 105 MB
- **THEN** 系统在下次写入触发或定时任务运行时删除最旧记录，直至占用 ≤ 100 MB

#### Scenario: 配置为 0 表示不清理

- **WHEN** `max_steps_size_mb = 0`
- **THEN** 系统 SHALL 跳过任何 step 清理逻辑，无论表占用多大

#### Scenario: 清理不影响用户消息

- **WHEN** 清理任务运行
- **THEN** 仅 `t_fv_ai_agent_steps` 表受影响，`t_fv_ai_sessions` / `t_fv_ai_messages` 不被删除
