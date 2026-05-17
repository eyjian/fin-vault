## 1. 准备与依赖

- [ ] 1.1 在 `backend/go.mod` 添加 `trpc.group/trpc-go/trpc-agent-go@v1.9.1`，`go mod tidy` 通过
- [ ] 1.2 在 `backend/configs/config.yaml` 增加 `ai.session.max_steps_size_mb`（默认 100）、`ai.session.history_window`（默认 20）
- [ ] 1.3 在 `backend/internal/bootstrap/config.go` 注册新配置项的默认值与校验
- [ ] 1.4 拉一个独立分支 `feature/replace-ai-with-trpc-agent-go` 并推送

## 2. 数据模型 & migration

> **机制说明**：项目使用 GORM AutoMigrate（`backend/internal/repository/gorm/automigrate.go`），无 SQL 迁移文件机制。本议题"建表"= 新增 domain struct + 加入 AutoMigrate 列表；"删表"= 删除 domain struct + 从 AutoMigrate 列表移除（不需要 DROP TABLE 脚本，按 design.md "Migration Plan" 第 5 条本议题不做向下兼容）。
>
> **范围说明**：本议题用户已确认无老 AI 数据需要兼容，故 §2 阶段同时完成"建新 AI 三表"与"删旧 AI 模块（AIConversation / AIMessage 及其 service / handler / wire）"，让出 `t_fv_ai_messages` 表名给新 `Message` 实体，避免命名冲突。原议题 §6/§7 中相关删除子项已合并到此处，不重复执行。

### 建新

- [ ] 2.1 在 `backend/internal/domain/` 新增 `Session` / `Message` / `AgentStep` 三个 struct（均不继承 `BaseModel`，因主键为 UUID 字符串列；按 naming-conventions skill 规范：每个字段显式 `column:` tag、显式实现 `TableName()`，详见 design.md D9）
- [ ] 2.2 在 `backend/internal/repository/gorm/automigrate.go` 的 AutoMigrate 列表追加 `*domain.Session`（建表 `t_fv_ai_sessions`：`f_id` varchar(36) PK / `f_user_id` / `f_title` / `f_created_at` / `f_updated_at` + 索引 `idx_user_updated(f_user_id,f_updated_at)`）
- [ ] 2.3 同列表追加 `*domain.Message`（建表 `t_fv_ai_messages`：`f_id` / `f_session_id` FK→`t_fv_ai_sessions.f_id` / `f_role` / `f_content` / `f_token_usage` JSON / `f_created_at` + 索引 `idx_session_created(f_session_id,f_created_at)`）
- [ ] 2.4 同列表追加 `*domain.AgentStep`（建表 `t_fv_ai_agent_steps`：`f_id` / `f_session_id` FK→`t_fv_ai_sessions.f_id` / `f_message_id` FK→`t_fv_ai_messages.f_id` / `f_event_type` / `f_tool_name` / `f_payload` JSON / `f_created_at` + 索引 `idx_step_session_created(f_session_id,f_created_at)`、`idx_created`；注：与 §2.3 的 `idx_session_created` 区分，因 SQLite 索引名为库级命名空间，跨表不能同名）
- [ ] 2.5 本地启动 `go run ./cmd/finvault` 触发 AutoMigrate，用 `sqlite3 data/finvault.db ".schema t_fv_ai_sessions"` / `".schema t_fv_ai_messages"` / `".schema t_fv_ai_agent_steps"` 核对字段类型、索引、外键完整

### 删旧（本议题用户已确认无老 AI 数据需要兼容）

- [ ] 2.6 在 `backend/internal/domain/ai_report.go` 中**精确删除** `AIConversation` 与 `AIMessage` 两个 struct 及其 `TableName()` 方法（保留同文件内的 `Report` struct！建议拆分文件：旧 AI 部分整体删除，`Report` 留在原文件或挪到 `ai_report.go` 重命名为 `report.go`，dev 自决最干净的拆法）
- [ ] 2.7 删除 `backend/internal/repository/gorm/ai_chat_repo.go`（一体仓储实现）；从 `backend/internal/repository/gorm/automigrate.go` 移除 `&domain.AIConversation{}` / `&domain.AIMessage{}` 两个 entry；删除 `backend/internal/repository/interfaces.go` 中 `AIConversationRepository` 接口与 `Repos.AIConversation` 字段；同步从 `backend/internal/testutil/mock_repos.go` 删除 `MockAIConversationRepo` 整段
- [ ] 2.8 删除 `backend/internal/service/chat_service.go` / `advisor_service.go` / `analysis_service.go` / `ai_services_test.go` 四个文件（确认这三个 service 仅承载旧 AI 逻辑，不含可独立保留的非 AI 业务；如发现可独立保留逻辑，**先回报架构师**，不擅自决策）
- [ ] 2.9 删除 `backend/internal/handler/chat_handler.go` / `advisor_handler.go` / `analysis_handler.go` 三个 handler；从 `backend/internal/bootstrap/wire.go` 删除 `Chat` / `Advisor` / `Analysis` handler 字段、`chatSvc` / `advisorSvc` / `analysisSvc` 三处装配、`repos.AIConversation` 注入、以及 router 上 `/api/v1/ai/chat` 等旧 AI 路由（保留 `AIMeta` handler 与 `GET /api/v1/ai/providers` 类元信息接口，那是新 AI 仍要用的）

### 收尾

- [ ] 2.10 `go vet ./...` + `go build ./...` + `go test ./...` 全绿；`pkg/errs/errs.go` 中 `ErrAIConversationNotFound`（错误码 50001）暂不删，留至 §7.1 自研 Provider 退场时一并评估；`pkg/utils/response/response.go` 引用同步处理

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

- [ ] 5.0 在 `pkg/errs/errs.go` 新增 `ErrAIProviderRateLimited`（50006）/ `ErrAIToolNotFound`（50007）两个错误码（已先期完成于 Step 5-pre commit；dev 在 §5.4 错误映射时直接复用，不重做）
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

> **范围调整**：旧 ChatService / AdvisorService / AnalysisService（基于自研 Provider）已在 §2.8 删除，本节只剩"自研 Provider 文件退场 + 新 Service 新建"。

- [ ] 7.1 删除 `backend/internal/llm/openai_provider.go`、`registry.go`、`registry_test.go`、`provider.go`、`fake_provider.go`、`config.go`（自研 Provider 整体退场）；同步评估并按需删除 `pkg/errs/errs.go` 中已无引用的旧 AI 错误码（如 `ErrAIConversationNotFound` / `ErrAIRequestFailed` 等）及 `response.go` 引用
- [ ] 7.2 新建 `service/ai_session_service.go`：会话 CRUD、用户隔离校验
- [ ] 7.3 新建 `service/ai_message_service.go`：调用 `Runner.Run`，落 user/assistant 消息，串起 step 事件
- [ ] 7.4 在 service 层禁止 import `trpc-agent-go`（用 lint 规则或 review 把关）
- [ ] 7.5 service 单测：用 mock Runner + in-memory store

## 8. Handler & 路由

> **范围调整**：旧 `chat_handler.go` / `advisor_handler.go` / `analysis_handler.go` 与 `/api/v1/ai/chat` 等旧路由已在 §2.9 删除，本节只新增。

- [ ] 8.1 新增 `handler/ai_session_handler.go`：`POST/GET/DELETE /api/v1/ai/sessions`、`GET /api/v1/ai/sessions/{id}/messages`
- [ ] 8.2 新增 `handler/ai_message_handler.go`：`POST /api/v1/ai/sessions/{id}/messages`，响应体含 `tool_calls` 数组
- [ ] 8.3 新增 `GET /api/v1/ai/providers`：列所有可用 Provider（如已存在 `AIMetaHandler` 提供该路由，则在新 Runner 装配后接管即可，不重复实现）
- [ ] 8.4 路由统一挂在已有的 `auth` 中间件下，未登录返回 401
- [ ] 8.5 e2e：用 `httptest` 跑通 5 步流程（建会话 → 多轮 → search_fund → market_quote → 删会话）

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
