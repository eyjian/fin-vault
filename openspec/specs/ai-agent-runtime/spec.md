# ai-agent-runtime Specification

## Purpose
TBD - created by archiving change replace-ai-with-trpc-agent-go. Update Purpose after archive.
## Requirements
### Requirement: 基于 trpc-agent-go 的 Agent 运行时

系统 SHALL 使用 `trpc.group/trpc-go/trpc-agent-go` 作为唯一的 AI Agent 运行时框架，封装在 `backend/internal/llm/agent` 包内对外暴露 `Runner` 接口。

#### Scenario: 单例 Runner

- **WHEN** 后端启动
- **THEN** Bootstrap 阶段 SHALL 构造唯一的 Agent Runner 实例并注入到 Service 层
- **AND** 整个进程生命周期内不重复创建 Runner

#### Scenario: 框架被替换时不影响业务层

- **WHEN** 未来替换底层 Agent 框架（如升级到下一代）
- **THEN** Service / Handler 层代码 SHALL 不直接 import `trpc-agent-go` 的任何包，仅依赖本项目定义的 `Runner` 接口

### Requirement: 多 Provider 路由（OpenAI 兼容）

系统 SHALL 通过 OpenAI 兼容协议适配多家模型服务商，并按配置 `llm.default` 选择默认 Provider。

#### Scenario: 默认走 DeepSeek

- **WHEN** 配置 `llm.default = "deepseek"` 且 DeepSeek 凭证有效
- **THEN** Agent 推理 SHALL 通过 DeepSeek 的 OpenAI 兼容端点完成

#### Scenario: 默认 Provider 不可用时按列表 fallback

- **WHEN** 默认 Provider 缺少必需配置（缺 `api_key` 或 `model`）
- **THEN** 系统 SHALL 按 `providers` map 中可用 Provider 的字典序选取一个作为默认，并在启动日志记录 fallback 原因

#### Scenario: Provider 列表可枚举

- **WHEN** 调用 `GET /api/v1/ai/providers`
- **THEN** 响应 SHALL 返回所有已配置 Provider 的名称、模型名、是否为默认

### Requirement: 步骤事件持久化

系统 SHALL 将 Agent 每一步的关键事件（`tool_call_started`、`tool_call_finished`、`token_usage`、`step_boundary`）写入 `ai_agent_steps` 表，便于前端展示与故障排查。

#### Scenario: 工具调用产生 step 记录

- **WHEN** Agent 在一次 turn 中调用工具 `search_fund`
- **THEN** `ai_agent_steps` SHALL 至少新增 2 行：`event_type = tool_call_started`、`event_type = tool_call_finished`
- **AND** 每行包含 `session_id`、`message_id`、`tool_name`、`payload`（JSON）、`created_at`

#### Scenario: token 用量被记录

- **WHEN** 一次 LLM 调用结束
- **THEN** 系统 SHALL 写入一条 `event_type = token_usage` 的 step，`payload` 内含 `prompt_tokens`、`completion_tokens`、`total_tokens`
- **AND** 同时累计写入 `ai_messages.token_usage` 字段（assistant 消息）

### Requirement: 错误统一映射为业务错误码

系统 SHALL 将 Agent 框架抛出的错误（模型超时、工具异常、上下文超限等）转换为项目统一的业务错误码并记录日志（含文件名+行号）。

#### Scenario: 上游 429 限流

- **WHEN** Provider 返回 HTTP 429
- **THEN** 系统 SHALL 返回业务错误码 `AI_PROVIDER_RATE_LIMITED`，HTTP 状态 `503`，日志 `level=warn`

#### Scenario: 工具内部 panic

- **WHEN** 工具执行抛出 panic
- **THEN** Agent SHALL 捕获并返回业务错误码 `AI_TOOL_FAILED`，HTTP 状态 `500`，日志 `level=error` 含完整堆栈
- **AND** 当前会话 SHALL 保持可继续状态，下条消息仍可发送

