## Why

当前后端 AI 模块（`backend/internal/llm/`）是基于 `http.Client` 自研的 OpenAI 兼容 Provider，已经能完成单轮问答与本地工具调用，但存在三大结构性缺陷：

1. **无会话记忆** —— 每次请求都是无状态调用，前端虽有"新建会话"按钮但后端不存历史，"豆包/DeepSeek 那样的多轮对话"无法实现。
2. **工具调用是手写解析** —— `internal/llm/tools/*.go` 的 5 个工具都靠手工拼 JSON Schema、手工解析 `tool_calls`，新增"查询上证指数"、"查询指定基金"这类能力时心智成本高、容易出错。
3. **缺少 Agent 运行时** —— 没有 step/turn/tool-loop 这一层抽象，未来想加 skill、planner、RAG 等高级能力都要从零造轮子。

社区在过去一年已经形成了成熟的 Go 端 Agent 框架，其中 `trpc-group/trpc-agent-go` 已发布 v1.9.1（截至 2026-05-17 仍在活跃迭代），文档完整、原生支持 OpenAI 兼容模型、内置 Session/Memory/Tool/Runner 抽象，是当前阶段最稳妥的选型。Eino 仍处于 `v0.9.0-alpha.x`，不适合生产。

由于项目仍处 phase-0、尚未上线、AI 模块在 [REQUIREMENT.md](../../../REQUIREMENT.md) 中定位为高频核心入口，**现在做一刀切替换的成本最低、收益最大**。

## What Changes

### 新增

- 引入 `trpc.group/trpc-go/trpc-agent-go` 作为 Agent 运行时框架。
- 新增 **会话记忆** 能力：基于 SQLite 持久化 `sessions` / `messages` / `agent_steps` 三张表，支持"新建会话""会话列表""按会话续聊"。
- 新增 **会话清理** 能力：按 `max_steps_size_mb` 配置上限自动滚动清理最旧的 step 记录（值为 `0` 时不清理）。
- 新增 **基金/行情查询工具**：`search_fund`（按代码/名称模糊搜本地基金库）、`market_quote`（取上证指数等行情）。
- 新增 **Agent 步骤可观测**：保留 `tool_call` 状态、token 用量、step 边界 三类事件并落库，前端可展示"工具调用过程"。

### 修改

- **BREAKING** `internal/llm/` 从"自研 Provider + 手写 tools"重写为"trpc-agent-go Runner + Model 适配器 + Tool 适配器"。
- **BREAKING** AI 相关 HTTP 接口语义变化：从单次 `POST /api/v1/ai/chat` 变为 `POST /api/v1/ai/sessions`（建会话）+ `POST /api/v1/ai/sessions/{id}/messages`（发消息）+ `GET /api/v1/ai/sessions`（列表）。
- **BREAKING** 旧的 AI 历史记录（如有）不做迁移，直接清空。
- 配置文件 `llm.*` 段保留多 Provider 路由能力（`default` + `providers` map），通过 trpc-agent-go 的 OpenAI 兼容 Model 适配多家服务商（DeepSeek / Qwen / GLM / Kimi / Ollama）。

### 移除

- 删除 `internal/llm/openai_provider.go`、`internal/llm/registry.go`（含 `registry_test.go`）的自研 HTTP 实现。
- 删除 `internal/llm/tools/registry.go` 自研工具注册中心，改用 trpc-agent-go 的 `tool.Tool` 接口。
- 删除 `internal/service/ai_services_test.go` 中针对自研 Provider 的测试，改为针对 Agent Runner 的测试。

## Capabilities

### New Capabilities

- `ai-session`：AI 多轮会话与持久化记忆；包含会话 CRUD、消息追加、按会话拉取上下文、按 `max_steps_size_mb` 滚动清理。
- `ai-agent-runtime`：基于 trpc-agent-go 的 Agent 运行时；包含 Runner、Model 适配（OpenAI 兼容）、Tool 注册、step/turn 事件、token 计量。
- `ai-tools`：AI 可调用的业务工具集合；包含 `search_fund`、`market_quote`、`history_query`、`holding_query`、`profit_calc`、`platform_summary` 等。

### Modified Capabilities

> 当前 `openspec/specs/` 为空（项目首个 OpenSpec 议题），无既有 spec 可改。本议题创建的 3 个 capability 将作为 AI 模块的初始 spec 基线。

## Impact

### 受影响代码

- `backend/internal/llm/`：整个目录重写。
- `backend/internal/llm/tools/`：6 个工具文件按 `trpc-agent-go` 的 `tool.Tool` 接口重写。
- `backend/internal/service/`：`ai_services*.go` 重写为 SessionService + AgentService。
- `backend/internal/handler/`：AI 相关 handler 拆为 `session_handler.go` + `message_handler.go`。
- `backend/internal/domain/`：新增 `Session` / `Message` / `AgentStep` 领域模型。
- `backend/internal/bootstrap/`：`config.go` 增加 `ai.session.*` 段，注册 Runner 单例。
- `backend/migrations/`：新增 `0xx_create_ai_sessions.sql` 等。
- `frontend/`：AI 页面调用接口改为会话化（议题范围内只列接口契约，前端实现作为后续任务）。

### 受影响依赖

- 新增 `trpc.group/trpc-go/trpc-agent-go`（v1.9.1+）。
- 移除自研 Provider 对 `net/http` 的直接依赖（仍间接保留）。

### 受影响接口（BREAKING）

| 旧接口 | 新接口 |
|---|---|
| `POST /api/v1/ai/chat` | `POST /api/v1/ai/sessions` + `POST /api/v1/ai/sessions/{id}/messages` |
| 无 | `GET /api/v1/ai/sessions` |
| 无 | `GET /api/v1/ai/sessions/{id}/messages` |
| 无 | `DELETE /api/v1/ai/sessions/{id}` |

### 数据库

- 新增 3 张表：`ai_sessions`、`ai_messages`、`ai_agent_steps`。
- 不迁移旧数据（项目未上线）。

### 配置

- 保留：`llm.default`、`llm.providers.<name>.{api_key, base_url, model}`。
- 新增：`ai.session.max_steps_size_mb`（int，默认 `100`，`0` 表示不清理）、`ai.session.history_window`（int，默认 `20` 条消息）。
