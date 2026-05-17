## Context

`fin-vault` 处于 phase-0，AI 模块当前在 `backend/internal/llm/` 自研：用 `net/http` 直连 OpenAI 兼容端点，工具调用解析手写。已经能完成单轮问答与基础工具调用，但前端"新建会话""会话历史""上证指数""指定基金"四个能力都拿不到后端支持，再叠加上未来 skill / planner / RAG 的需求，自研路径成本越来越高。

社区在 2025-2026 已形成两个主流 Go 端 Agent 框架：

| 框架 | 最新版本 | 评估 |
|---|---|---|
| `cloudwego/eino` | `v0.9.0-alpha.24` | 仍在 alpha，API 不稳定，**不选** |
| `trpc-group/trpc-agent-go` | `v1.9.1` | 已发布稳定版，迭代活跃，文档全 |

由用户在前序对话中已敲定使用 **trpc-agent-go**，并选择"一刀切替换 + 不做向下兼容"。

## Goals / Non-Goals

**Goals:**

- 用 `trpc-agent-go` 替换 `internal/llm/` 自研实现，引入 Session、Memory、Tool、Runner 抽象。
- 实现"豆包/DeepSeek 那样"的多轮对话：会话列表 + 新建会话 + 按会话续聊。
- 多 Provider 路由保留：`llm.default` 选默认，`llm.providers.<name>` 列出可选，全部走 OpenAI 兼容端点。
- 工具调用规范化：每个工具一个文件，结构体自动生成 schema，新增工具零侵入。
- 单机可跑：仅依赖 SQLite，不引入 Redis / 外部消息队列。
- 后续可平滑加 Redis 缓存：在 Session/Memory 接入处保留接口抽象（`SessionStore` / `Cache`），不做架构大改。
- 步骤可观测：`tool_call`、`token_usage`、`step_boundary` 三类事件全部落库。

**Non-Goals:**

- 不做旧 AI 数据迁移（项目未上线）。
- 不引入 Redis / RAG / 向量库（议题范围之外）。
- 不实现流式 SSE 推送（首版返回完整响应即可，下个议题再加）。
- 不重写前端（议题只定后端契约，前端改造作为后续任务）。
- 不做 Provider 自动健康检查 / 熔断（启动时一次性 fallback 即可）。

## Decisions

### D1：选 trpc-agent-go，弃 Eino

**选择**：`trpc.group/trpc-go/trpc-agent-go` v1.9.1+

**理由**：

- Eino 仍在 `0.9.0-alpha.24`，公开发版 24 个 alpha 仍未到 RC，API 频繁破坏。
- trpc-agent-go 已 v1.9.1，文档齐全，社区活跃（腾讯 trpc 团队主推）。
- 内置 Session / Memory / Tool / Runner 四件套，与本议题需求高度对齐。

**备选**：LangChainGo（社区版 Go 实现不完整）、自研继续演进（成本高且偏离主流）。

### D2：一刀切替换，不做向后兼容

**选择**：直接删 `internal/llm/openai_provider.go`、`registry.go`、`tools/registry.go`，HTTP 接口路径变化。

**理由**：项目未上线，没有外部调用方，保留旧路径反而增加心智成本。

**备选**：双跑 + feature flag（被否：增加 Service 层 if/else，长期债）。

### D3：Provider 路由用"OpenAI 兼容 Model 适配器"

**选择**：所有 Provider（DeepSeek / Qwen / GLM / Kimi / Ollama）通过同一个 `openai.New(...)` 风格的 Model 实例化，仅 `BaseURL` / `APIKey` / `Model` 不同。

**理由**：6 家服务商均提供 OpenAI 兼容端点，配一份代码即可全部支持。

**备选**：每家一个 Provider 实现（被否：N×N 维护负担）。

### D4：会话存储用 SQLite，预留 Cache 接口

**选择**：

- 持久化层：3 张表（`t_fv_ai_sessions` / `t_fv_ai_messages` / `t_fv_ai_agent_steps`）落 SQLite。
- 业务层定义 `SessionStore` 接口；当前唯一实现是 `SQLiteSessionStore`。
- 在 `SessionStore` 上方插一个可选 `Cache` 接口（默认 `NoopCache`），后续接 Redis 时只换实现。

**理由**：单机跑成本低，又给未来留口子（用户要求"后续增加不要架构大改动"）。

**备选**：直接走 trpc-agent-go 自带的 InMemorySession（被否：进程重启即丢，无法满足"会话历史"需求）。

### D5：历史窗口 + step 滚动清理双策略

**选择**：

- 短期上下文：`ai.session.history_window`（默认 20 条）控制送给模型的消息数。
- 长期容量：`ai.session.max_steps_size_mb`（默认 100MB，0 = 不清理）控制 `t_fv_ai_agent_steps` 表大小。
- 清理只针对 `t_fv_ai_agent_steps`，不影响用户消息（避免破坏对话连续性）。

**理由**：消息表是用户资产不可丢；step 表是审计日志可丢。两套阈值分开管理。

### D6：工具 schema 用反射自动生成

**选择**：工具入参定义为 Go 结构体 + `json` / `description` tag，运行时反射生成 JSON Schema。

**理由**：trpc-agent-go 已内置该能力；新工具只需写一个 `*.go` 文件即可上线。

### D7：错误统一映射 + 日志带文件名行号

**选择**：复用 [response.go](../../../backend/pkg/utils/response/response.go) 的业务错误码体系，在 Service 层把 Agent 框架的错误转换为如 `AI_PROVIDER_RATE_LIMITED` / `AI_TOOL_FAILED` / `AI_TOOL_NOT_FOUND` 等枚举值；日志走已有的滚动日志（已带文件名行号）。

### D8：包结构

```
backend/internal/llm/
├── agent/
│   ├── runner.go          # Runner 接口 + 默认实现（包装 trpc-agent-go）
│   ├── runner_trpc.go     # trpc-agent-go 适配实现（唯一 import 框架的文件）
│   └── runner_test.go
├── model/
│   ├── factory.go         # 按配置生成 OpenAI 兼容 Model
│   └── factory_test.go
├── session/
│   ├── store.go           # SessionStore 接口
│   ├── sqlite_store.go    # SQLite 实现
│   ├── cache.go           # Cache 接口（含 NoopCache，预留 Redis）
│   └── cleanup.go         # max_steps_size_mb 清理任务
└── tools/
    ├── search_fund.go     # 新增
    ├── market_quote.go    # 新增
    ├── history_query.go   # 改造
    ├── holding_query.go   # 改造
    ├── profit_calc.go     # 改造
    └── platform_summary.go # 改造
```

业务代码（service / handler）只 import `agent` 与 `session` 两个包，**绝不直接 import trpc-agent-go**。

### D9：会话主键采用 UUID 字符串列（`varchar(36)`）

**选择**：

- `t_fv_ai_sessions.f_id` 类型：`varchar(36)`，存 RFC 4122 UUID 标准串（带连字符），由 `google/uuid` 在 service 层生成。
- `t_fv_ai_messages.f_id` / `t_fv_ai_agent_steps.f_id` 同样使用 `varchar(36)` UUID，便于跨表关联与日志查询。
- 外键 `f_session_id` / `f_message_id` 同为 `varchar(36)`。
- domain struct **不继承** `domain.BaseModel`（后者是 `uint autoIncrement`），需自定义 `f_id` 字段并显式声明 `gorm:"column:f_id;primaryKey;type:varchar(36)"`。

**理由**：

- 跨 SQLite / PG / MySQL 兼容（PG 有 native UUID，但 `varchar(36)` 是三家共同语义最稳的选择）。
- spec.md 明确要求 `session_id` 为 UUID 字符串（API 层直接返回不做转换）。
- 与现有 `BaseModel` 的 `uint autoIncrement` 路线分流是必须的；为此 ai-session 三张表是 fin-vault 命名规范中**唯一允许 `f_id` 非 `bigint` 的例外**，该例外由本议题 design.md D9 显式背书，不破坏 naming-conventions skill 的全局规则。

**备选**：

- `bigint` 自增 + 业务 UUID 单独列：被否，多一个列徒增心智成本。
- PG 原生 `uuid` 类型：被否，SQLite 没对应类型，跨库不一致。

### D10：旧 AI 模块（自研 Provider 配套）在 §2 阶段一并删除

**选择**：

本议题 §2 阶段同时完成"建新 AI 三表"与"删旧 AI 模块"，删除范围：

- domain：`ai_report.go` 中 `AIConversation` / `AIMessage` 两个 struct（保留 `Report`）
- repository：`gorm/ai_chat_repo.go` 整文件、`interfaces.go` 中 `AIConversationRepository` 接口与 `Repos.AIConversation` 字段、`gorm/automigrate.go` 中两个 entry
- service：`chat_service.go` / `advisor_service.go` / `analysis_service.go` / `ai_services_test.go`
- handler：`chat_handler.go` / `advisor_handler.go` / `analysis_handler.go`
- bootstrap：`wire.go` 中 `Chat` / `Advisor` / `Analysis` handler / svc 装配 + 路由
- testutil：`mock_repos.go` 中 `MockAIConversationRepo`

**理由**：

- 用户已确认无老 AI 数据需要兼容，没有"双轨过渡期"需求。
- 旧 AI 模块的 `t_fv_ai_messages` 表名与本议题新表强冲突，必须先让出名字；不前置删除将被迫使用 `_v2_` 等丑陋前缀，违反 naming-conventions。
- 把"删旧"集中在 §2 一次完成，避免 §6/§7 反复触碰同一处代码，降低 review 噪音。

**范围调整**：

原 tasks.md §7.1 / §7.2 / §8.1 中"删除 ChatService 等"挪入 §2.6～§2.9，§7/§8 仅保留"自研 Provider 文件退场 + 新代码新建"两类。详见 tasks.md 当前版本。

**备选**：

- 双轨保留旧 AI 直至 §7 再删（被否：表名冲突无法绕开）。
- 用 `_v2_` 等前缀做新表（被否：长期债，违反命名规范且 §6/§7 还得回头清理）。

### D11：SDK 错误映射策略

**场景与映射**（统一在 `backend/internal/llm/agent/error_mapping.go` 内实现，独立可测）：

| 来源 | SDK 表现 | 业务错误码 | HTTP |
|---|---|---|---|
| 工具内部 **panic** | event_handler 在消费 channel 时 recover 到 panic | `ErrAIToolCallFailed`（50005） | 500 |
| 工具返回 **error**（非 panic） | event 中 ToolCall 带错误信息 | **不返回错误**：在聚合的 `[]ToolCall` 中把对应项 `Status="failed"` + 填 `ErrorMessage`，业务 `Runner.Run` 仍 success | — |
| 上游 LLM Provider 4xx 限流（HTTP 429） | SDK 透传 OpenAI 兼容协议错误 | `ErrAIProviderRateLimited`（50006） | 503 |
| 上游 LLM Provider 5xx | SDK 透传 5xx | `ErrAIRequestFailed`（50004） | 500 |
| 调用未注册工具 | SDK 抛 unknown tool 类错误 | `ErrAIToolNotFound`（50007） | 400 |

**实现要点**：

- `event_handler.go` 消费 SDK event channel；识别 SDK 错误类型后调用 `error_mapping.go` 的 `MapSDKError(err) errs.AppError`。
- 工具 error vs panic 的差别在 SDK event 中已区分（panic 走 recover 路径，error 进 ToolCall.Error 字段）；event_handler 必须区别处理：panic → 整体失败；error → ToolCall.Status="failed" 但 Run 仍成功。
- 区分 4xx 限流 vs 其它 4xx：依据 SDK error 的 HTTP status 字段（OpenAI 兼容 client 通常透传），仅 429 走 `ErrAIProviderRateLimited`。
- 未识别的错误兜底为 `ErrAIRequestFailed`。

**理由**：

- 五个错误码（50004 / 50005 / 50006 / 50007 + 工具失败软错误）覆盖 design 摸底报告中评估的全部错误路径。
- 工具失败软错误（不返回 error 但标记 Status=failed）与 OpenAI Function Calling 语义对齐：工具失败是模型可处理的信号，不该把整次 Run 拖垮，让 assistant 有机会基于失败信号继续决策（重试 / 询问用户 / 走兜底分支）。
- 集中在 `error_mapping.go` 实现，单测可覆盖每个分支，无需启 SDK 真正调用。

### D12：业务 Runner 与 SDK 衔接方案 A（inmemory session.Service）

**选择**：

业务层的 `session.SessionStore`（D8 / §3.3 / §4）与 SDK 的 `session.Service` **是两套独立模型**，不互相代理。具体：

- SDK Runner 构造时注入 SDK 自带的 `inmemory.NewSessionService()`（每次 `Runner.Run` 内的 working memory，请求结束即丢弃）。
- 业务持久化路径完全由我们的 `SessionStore` 控制，与 SDK 解耦。

**§5.2 五步流程**（`agent/runner_trpc.go` 内实现）：

1. service 层调业务 `Runner.Run(ctx, sessionID, userMsg)`
2. 业务 Runner 内：① 调 `SessionStore.ListMessages(ctx, sessionID, 0)` 拉最近 N 条历史（N = config.AI.Session.HistoryWindow）
3. ② 把历史 + 当前 userMsg 转换为 SDK `model.Message[]` 序列
4. ③ 调 SDK `runner.NewRunner(...)` 创建 Runner 后调 `Run(ctx, userID, sessionID, model.Message)` 拿 `<-chan *event.Event`
5. ④ `event_handler` 消费 channel：聚合 assistant message text、收集 ToolCall（带 started/finished/status/error_message）、累加 TokenUsage
6. ⑤ 按 step 事件分别落库：
   - 每个 ToolCall start/end 事件 → `SessionStore.AppendStep(ctx, AgentStep{event_type, tool_name, payload})`，payload 写库前过 mask
   - assistant 最终消息 → `SessionStore.AppendMessage(ctx, Message{role="assistant", content, token_usage})`
   - user 消息（入参的 userMsg）也由业务 Runner 在步骤 ① 之前调用 AppendMessage 落库，**不依赖 service 层落 user 消息**（避免 service 与 Runner 双写竞争）
7. ⑥ 返回业务接口聚合结果 `(assistantMsg, []ToolCall, TokenUsage, error)`

**红线**（与 D8 一致）：

- trpc-agent-go 的 import 仅出现在 `internal/llm/agent/`、`internal/llm/model/` 与 `internal/llm/tools/` 包内（tools 包通过 trpc-agent-go function 子包定义 LLM 工具，schema 由 `json` + `jsonschema` tag 自动反射）。其它任何业务代码（service / handler / domain / repository / bootstrap / middleware / ...）禁止 import trpc-agent-go。
- §5 验收时由 tester 跑 `grep -r "trpc.group/trpc-go/trpc-agent-go" backend/internal/{service,handler}/` 确认 0 命中。

**理由**：

- SDK 的 session.Service 抽象偏向"SDK 内部状态"，与我们持久化语义（用户隔离 / history_window / step 落库 / mask）耦合度低；做适配器需要双向数据转换，工作量大且增加心智负担。
- inmemory session.Service 的"working memory"特性正好匹配单次 Run 的语义，请求结束即丢弃，不残留任何状态。
- 业务 Runner 自管 message 持久化让 mask / history_window / 用户隔离逻辑全部集中在 `SessionStore`，符合单一职责。

**备选**：

- 实现 `session.Service` 适配器，把 SDK Service 调用代理到我们的 SessionStore（被否：双向适配代码量翻倍，session 模型语义不完全对齐，调试困难）。
- 直接使用 SDK 的 PG/MySQL session.Service 实现（被否：fin-vault 主存 SQLite + 业务表用 GORM AutoMigrate，引入 SDK 的另一套表破坏数据模型一致性）。

### D13：工具用户隔离强制约束

**背景**：

`spec ai-tools` 的 `holding_query 仅看本人持仓` Scenario 是**行为级**约束（"工具应仅返回 user_id=A 的持仓"），D13 是**实现级**约束（"通过什么工程手段保证行为级约束不被绕过"）。

`backend/internal/llm/tools/holding_query.go` 在改造前存在高危越权漏洞：

- 入参 schema 暴露 `user_id` 字段，让 LLM 看到这个参数
- `user_id == 0` 时**默认 1**作为兜底（"无身份兜底为 user 1"，等于把所有匿名调用都路由到 user 1 的数据）

恶意 prompt 可以让 LLM 传 `user_id=2` 越权访问别人持仓；恶意上游 service 调用不带 user_id 时也会被默认到 user 1。**§6.3 改造时一并修复**，不开独立 hotfix commit。

**强制规则**（reviewer 看到违反直接打回）：

1. **入参 schema 禁止 `user_id` 字段**：凡涉及用户数据的工具（`history_query` / `holding_query` / `market_data` / `profit_calc` / `platform_summary` 及未来新增同类工具），其 Args struct **不得包含 `UserID` / `user_id` 字段**——既不让 LLM 看到该参数，也禁止 LLM 传入。schema 干净地只暴露过滤参数（asset_type / platform_id / status 等业务维度）。

2. **fn 必须从 ctx 提取 user_id**：工具实现的 fn 函数体**必须**调用 `tools.UserIDFromContext(ctx)` 提取身份；提取失败（key 不存在 / 类型不对 / 值为 0）**直接返回错误**，禁止"无身份兜底"等危险默认值。错误用 `errs.ErrAIToolCallFailed.WithMsg("user_id not in context")` 或等价 `fmt.Errorf` 兜底。

3. **ctxKey 类型 unexported**：在 `backend/internal/llm/tools/context.go` 内定义：

```go
package tools

import "context"

type ctxKeyType struct{}

var ctxKeyUserID = ctxKeyType{}

// WithUserID 在 ctx 中注入 user_id，供工具 fn 内强制隔离使用。
// service 层在调业务 Runner.Run 之前调用此函数。
func WithUserID(ctx context.Context, uid uint) context.Context {
    return context.WithValue(ctx, ctxKeyUserID, uid)
}

// UserIDFromContext 从 ctx 提取 user_id；不存在或为 0 返回 (0, false)。
// 工具 fn 必须用此函数获取身份，禁止读 args.UserID。
func UserIDFromContext(ctx context.Context) (uint, bool) {
    v, ok := ctx.Value(ctxKeyUserID).(uint)
    if !ok || v == 0 {
        return 0, false
    }
    return v, true
}
```

ctxKey 类型 unexported 防止其它包伪造 key 绕过隔离；只通过 `WithUserID` 注入。

4. **未来新增涉用户工具必须遵守 D13**：新工具 PR review 时，先看入参 struct 字段；如含 `user_id` / `userID` 等字段直接打回，让作者重写。tester 验收 §6 时跑 grep 验证 7 个现有工具入参全部不含 user_id 字段。

**理由**：

- 单一身份注入路径（service → ctx → 工具）杜绝身份伪造与"忘记过滤"两类典型漏洞
- 入参 schema 不含 user_id，LLM 物理上看不到这个参数，prompt injection 攻击面消失
- ctxKey unexported 让"非法注入"在编译期就过不了（外包写不出 `ctx.WithValue(SomeKey, ...)` 拿到正确的 key）

**备选**：

- 在每个工具 fn 内手动重复 user_id 校验逻辑（被否：散落实现，易遗漏；新工具作者忘记加校验是常见疏忽）
- 在 SDK 层拦截（被否：SDK 不感知业务身份概念，跨 SDK 升级风险高）

### D14：step.MessageID 关联策略（补 §5 设计遗漏）

**背景**：

- spec ai-agent-runtime 隐含 `step.MessageID` 关联到 assistant message 的契约：前端在某条 assistant 消息下展开 tool_calls 详情时，依据 `t_fv_ai_agent_steps.f_message_id` 关联回该消息
- domain.AgentStep 的 `f_message_id` 字段在 §2.1 设计为 `varchar(36) not null`（强制非空 + 36 位 UUID 长度）
- §5 实现遗漏：`agent/event_handler.go` 的 `appendStepSafe` 函数 5 处调用**全部不传 messageID**，写入时 step.MessageID 取 GORM 默认值（空字符串），违反 schema not null 语义；同时让 step 无法关联到任何 assistant message
- §5 tester 验收时未断言此字段非空，漏检通过

**规则**（service / agent 层共同遵守）：

1. **service 预生成 assistantMessageID**：`ai_message_service.go` 在调 `Runner.Run` 之前调用 `assistantMessageID := uuid.NewString()`
2. **service 通过 helper 注入 ctx**：调用 `agent.WithAssistantMessageID(ctx, assistantMessageID)` —— 该 helper 在 agent 包内 unexported `ctxKey` 私有化（与 D13 ctxKey 模式一致）
3. **event_handler.appendStepSafe 签名扩展**：函数新增 `messageID string` 参数；所有 5 处调用站点更新；从 ctx 提取 messageID 失败时降级为空字符串 + warn 日志，**不阻塞主流程**（避免回归后阻塞已稳定的 Run 路径）
4. **runner_trpc.go 内 assistant message 写入用同一 messageID**：从 ctx 提取 assistantMessageID（service 已注入），赋给 `domain.Message.ID`；保证 `step.MessageID` 与 `assistant_message.ID` 一致，前端可通过 messageID 反查关联 step 列表

**理由**：

- spec not null 合规：`f_message_id varchar(36) not null` 字段必须有效填充，不能为空字符串
- 前端 step ↔ message 关联可靠：UI 在某条 assistant 消息节点下展开 tool_calls 详情有了稳定的 join key
- 改动小且收敛：runner_trpc.go +5 行（提取 ctx + assistantMessageID 赋给 Message.ID）/ event_handler.go appendStepSafe 加 1 个 messageID 参数 + 5 处调用更新；agent 包内私有 ctxKey 模式与 D13 一致
- service 自管 messageID 生成符合 D12 五步流程演进：service 是身份与 ID 注入的统一来源

**备选**：

- 留 §10 文档化 + 前端按 sessionID + timestamp 近似关联（被否：spec not null 合规问题不能拖延；近似关联在并发场景下不可靠）

**风险**：

- 触碰 §5 已"完成"代码（runner_trpc.go + event_handler.go）—— 本质是补 §5 设计漏洞而非返工，触碰范围限于 agent 包内；不会重新破坏 §5 验收已通过的 D11/D12 行为契约
- 扩展 appendStepSafe 签名是 §5 接口面变更—— 该函数 unexported 仅 agent 包内调用，无外部消费者，影响范围限于本议题

### D15：AI handler X-User-Id 强校验（缺失即 401）

**背景**：

- spec ai-session §13-17 明确："未登录用户调用 `POST /api/v1/ai/sessions` → 401 Unauthorized + 不创建任何记录"
- 当前 `middleware.Auth` 在 `Mode="local"` 下走 `X-User-Id` Header 取值；缺失或非法时 fallback 到 `DefaultUserID=1`（默认）
- 当前 `handler/common.go` 的 `userIDFromHeader(c)` 同样 fallback 到 1 —— 在 asset/holding/transaction 等核心路由是合理的（单用户模式默认本人操作）
- 但 AI 路由不能 fallback：fallback 等于"任何匿名请求都被当作用户 1"，违反 spec "未登录被拒绝" + D2 跨用户隔离前提（隔离的前提是 userID 真实可信）

**规则**：

1. **AI handler 用独立 helper**：在 `backend/internal/handler/common.go` 新增 `requireUserIDFromHeader(c *gin.Context) (uint, bool)` —— Header 缺失或解析失败或值为 0 时返 `(0, false)`；调用方判断 `!ok` 时立即 `response.Fail(c, errs.ErrUnauthorized)` 并 return
2. **覆盖范围**：仅 AI 路由（`/api/v1/ai/sessions/*`、`/api/v1/ai/sessions/:id/messages`）使用 `requireUserIDFromHeader`；`/api/v1/ai/providers`（meta 端点，无用户数据）保持现状用 `userIDFromHeader` 或不取 userID
3. **不动 middleware.Auth**：保持其 `local` 模式 fallback 行为不变，避免破坏现有所有非 AI 路由的兼容性（asset/holding/transaction 等仍能用空 Header 默认用户 1 工作）
4. **二阶段切 JWT**：当 `Mode="jwt"` 启用后，`requireUserIDFromHeader` 升级为 "JWT claims 取 sub → uint"，缺失/无效仍返 401；handler 调用方代码不变

**理由**：

- spec 合规：未登录 → 401（不走 fallback=1 假冒成功）
- 跨用户隔离前提：D2 用户隔离的有效性依赖 `userID` 真实可信；fallback=1 会让所有匿名请求被认为是用户 1 → D2 失效
- 影响最小：不动 middleware（受影响面小）、不动现有非 AI handler、新 helper 与 AI handler 一一对应

**备选**：

- 改 `middleware.Auth` 加 `RequireAuth` flag，AI 路由 group 单独挂带 `RequireAuth=true` 的 Auth → 工程量大、改动面广，且现 router.go 中所有路由共用同一个 Auth 中间件实例
- 在 `requireUserIDFromHeader` 内部直接 `c.AbortWithStatus(401)` → 避免，handler 应统一走 `response.Fail` 拿到结构化日志 + request_id

**风险**：

- 引入 AI 路由独有的认证风格 —— 可控：通过 helper 命名（`require*`）即可表明强校验意图；future 二阶段 JWT 全面切换后两种 helper 行为对齐

| 风险 | 缓解 |
|---|---|
| trpc-agent-go API 在 v1.x 微调导致升级痛 | 在 `agent` 包内严格隔离，业务层只看 `Runner` 接口 |
| SQLite 单文件并发写在多 Worker 下成瓶颈 | 单机阶段够用；`SessionStore` 接口已抽象，后续可换 PG |
| 自动 schema 生成对复杂结构体不友好 | 议题首发的 6 个工具入参均为扁平结构；后续复杂工具用 `json.RawMessage` 兜底 |
| 多 Provider 凭证泄漏到日志 | 工具/请求日志按 D7 在序列化前掩码 `api_key` / `token` 等敏感字段 |
| 一刀切替换影响开发节奏 | 议题独立分支；合入前 e2e 走通 5 个核心场景再 merge |
| `max_steps_size_mb` 监控开销 | 用 `pragma page_count * page_size` 估算，不做精确扫描；触发频率：每写入 100 条 step 估算一次 |
| 后续接 Redis 时改动量 | 已预留 `Cache` 接口，加一个 `RedisCache` 实现即可 |

## Migration Plan

1. 在 `feature/replace-ai-with-trpc-agent-go` 分支独立开发。
2. 按 tasks.md 顺序提交，每步保证 `go build ./...` + 单测通过。
3. 数据库变更走新增 migration 文件，不修改既有 migration。
4. 合并前在本地手工跑 5 条 e2e：
   - 新建会话 → 多轮问答 → 工具调用（search_fund） → 工具调用（market_quote） → 删除会话
5. **回滚策略**：项目未上线，回滚 = 回退分支；不需要数据迁移回滚脚本。

## Open Questions

- Q1：`trpc-agent-go` 的 OpenAI 兼容 Model 是否原生支持 Ollama？需在实现阶段验证；若不支持则 Ollama 单独适配。
- Q2：step 表清理任务用"写入触发"还是"独立 cron"？倾向写入触发（每写入 N 条估算一次），但若空间增长猛于估算频率可能滞后；实现阶段评估后定。
- Q3：工具调用展示给前端的 `arguments` 字段在脱敏后是否仍可读？敏感字段集合需要在实现阶段与前端对齐。
