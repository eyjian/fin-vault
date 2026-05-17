## 1. 准备与依赖

- [ ] 1.1 在 `backend/go.mod` 添加 `trpc.group/trpc-go/trpc-agent-go@v1.9.1`，`go mod tidy` 通过
- [ ] 1.2 在 `backend/configs/config.yaml` 增加 `ai.session.max_steps_size_mb`（默认 100）、`ai.session.history_window`（默认 20）
- [ ] 1.3 在 `backend/internal/bootstrap/config.go` 注册新配置项的默认值与校验
- [ ] 1.4 拉一个独立分支 `feature/replace-ai-with-trpc-agent-go` 并推送

## 2. 数据模型 & migration

- [ ] 2.1 在 `backend/internal/domain/` 新增 `Session` / `Message` / `AgentStep` 结构体
- [ ] 2.2 新建 migration 文件 `0xx_create_ai_sessions.sql`：建 `ai_sessions` 表（含 `id` UUID PK / `user_id` / `title` / `created_at` / `updated_at` 索引）
- [ ] 2.3 同 migration：建 `ai_messages` 表（`id` / `session_id` FK / `role` / `content` / `token_usage` JSON / `created_at` 索引）
- [ ] 2.4 同 migration：建 `ai_agent_steps` 表（`id` / `session_id` FK / `message_id` FK / `event_type` / `tool_name` / `payload` JSON / `created_at` 索引）
- [ ] 2.5 本地跑 migration、检查表结构正确

## 3. 包结构 & 接口抽象

- [ ] 3.1 新建目录 `backend/internal/llm/agent/`、`model/`、`session/`，按 design D8 落地
- [ ] 3.1.1 删除 backend/internal/llm/trpc_agent_placeholder.go（§1.1 临时占位文件，§3 真实代码 import trpc-agent-go 子包后失去作用）
- [ ] 3.2 在 `agent/runner.go` 定义业务侧 `Runner` 接口：`Run(ctx, sessionID, userMsg) (assistantMsg, []ToolCall, TokenUsage, error)`
- [ ] 3.3 在 `session/store.go` 定义 `SessionStore` 接口（CRUD + ListMessages + AppendMessage + AppendStep + EstimateStepsSize）
- [ ] 3.4 在 `session/cache.go` 定义 `Cache` 接口与 `NoopCache` 默认实现（预留 Redis 接入点）
- [ ] 3.5 跑 `go build ./...` 确保骨架可编译

## 4. SQLite 持久化实现

- [ ] 4.1 实现 `session/sqlite_store.go`：会话 CRUD + 仅返回 `user_id` 匹配的会话
- [ ] 4.2 实现 `AppendMessage` 与按 `history_window` 拉最近 N 条消息
- [ ] 4.3 实现 `AppendStep`：写 `ai_agent_steps` 一行，`payload` JSON 序列化前对敏感字段掩码
- [ ] 4.4 实现 `EstimateStepsSize`：用 SQLite `pragma page_count * page_size` 估算
- [ ] 4.5 实现 `session/cleanup.go`：当估算 > `max_steps_size_mb` 时按 `created_at ASC` 删旧 step；`max_steps_size_mb=0` 时整体跳过
- [ ] 4.6 单测：`sqlite_store_test.go` 覆盖 CRUD、用户隔离、history_window、清理边界 0、清理触发等场景

## 5. trpc-agent-go 适配

- [ ] 5.1 实现 `model/factory.go`：按 `llm.providers.<name>` 配置生成 OpenAI 兼容 Model；`llm.default` 不可用时按字典序 fallback 并记录 warn 日志
- [ ] 5.2 实现 `agent/runner_trpc.go`：包装 trpc-agent-go 的 Runner / Session / Tool，实现 `Runner` 接口
- [ ] 5.3 把所有 step 事件（tool_call_started / tool_call_finished / token_usage / step_boundary）通过 `SessionStore.AppendStep` 落库
- [ ] 5.4 错误映射：把上游 4xx/5xx、工具 panic、未知工具映射为 `AI_PROVIDER_*` / `AI_TOOL_*` 业务错误码
- [ ] 5.5 单测：mock trpc-agent-go 客户端，覆盖正常/限流/工具失败/未知工具四种路径

## 6. 工具改造

- [ ] 6.1 删除 `backend/internal/llm/tools/registry.go`、`util.go`（中心化注册表不再需要）
- [ ] 6.2 改造 `history_query.go` 适配 trpc-agent-go `tool.Tool` 接口，参数结构体 + json/description tag
- [ ] 6.3 改造 `holding_query.go`，`Run` 内强制按当前 `user_id` 过滤
- [ ] 6.4 改造 `profit_calc.go` / `platform_summary.go`
- [ ] 6.5 新增 `search_fund.go`：按 `keyword` 模糊匹配 `code`/`name`，结果上限 20
- [ ] 6.6 新增 `market_quote.go`：按 `symbol`（如 `sh000001`）查上证指数等行情
- [ ] 6.7 在 `agent/runner_trpc.go` 启动时把所有工具自动注册到 Runner，并打印工具清单日志
- [ ] 6.8 工具单测：每个工具一份 `*_test.go`，含成功 + 失败 + 脱敏三类用例

## 7. Service 层

- [ ] 7.1 删除 `backend/internal/llm/openai_provider.go`、`registry.go`、`registry_test.go`、`provider.go`、`fake_provider.go`、`config.go`（自研 Provider 整体退场）
- [ ] 7.2 删除/重写 `backend/internal/service/ai_services.go`、`ai_services_test.go`
- [ ] 7.3 新建 `service/ai_session_service.go`：会话 CRUD、用户隔离校验
- [ ] 7.4 新建 `service/ai_message_service.go`：调用 `Runner.Run`，落 user/assistant 消息，串起 step 事件
- [ ] 7.5 在 service 层禁止 import `trpc-agent-go`（用 lint 规则或 review 把关）
- [ ] 7.6 service 单测：用 mock Runner + in-memory store

## 8. Handler & 路由

- [ ] 8.1 删除旧的 `POST /api/v1/ai/chat` 路由与 handler
- [ ] 8.2 新增 `handler/ai_session_handler.go`：`POST/GET/DELETE /api/v1/ai/sessions`、`GET /api/v1/ai/sessions/{id}/messages`
- [ ] 8.3 新增 `handler/ai_message_handler.go`：`POST /api/v1/ai/sessions/{id}/messages`，响应体含 `tool_calls` 数组
- [ ] 8.4 新增 `GET /api/v1/ai/providers`：列所有可用 Provider
- [ ] 8.5 路由统一挂在已有的 `auth` 中间件下，未登录返回 401
- [ ] 8.6 e2e：用 `httptest` 跑通 5 步流程（建会话 → 多轮 → search_fund → market_quote → 删会话）

## 9. Bootstrap & 装配

- [ ] 9.1 在 `internal/bootstrap/` 装配：构造单例 Runner，注入 SessionService / MessageService
- [ ] 9.2 启动日志列出：默认 Provider、可用 Provider 列表、已注册工具清单、清理阈值
- [ ] 9.3 增加优雅关闭：进程退出前 flush in-flight step

## 10. 验收 & 收尾

- [ ] 10.1 `go vet ./...` + `go build ./...` + 全部单测通过
- [ ] 10.2 本地手工 e2e 走通 5 个核心场景
- [ ] 10.3 跑 `openspec validate replace-ai-with-trpc-agent-go --strict` 通过
- [ ] 10.4 更新 [docs/architecture-design.md](../../../docs/architecture-design.md)：AI 模块章节改为 trpc-agent-go 描述
- [ ] 10.5 更新 [docs/database-schema.md](../../../docs/database-schema.md)：补 3 张新表
- [ ] 10.6 PR 描述链回本议题路径 `openspec/changes/replace-ai-with-trpc-agent-go/`
- [ ] 10.7 合并后执行 `openspec archive replace-ai-with-trpc-agent-go`
