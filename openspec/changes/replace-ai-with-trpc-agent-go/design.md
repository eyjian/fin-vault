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

- 持久化层：3 张表（`ai_sessions` / `ai_messages` / `ai_agent_steps`）落 SQLite。
- 业务层定义 `SessionStore` 接口；当前唯一实现是 `SQLiteSessionStore`。
- 在 `SessionStore` 上方插一个可选 `Cache` 接口（默认 `NoopCache`），后续接 Redis 时只换实现。

**理由**：单机跑成本低，又给未来留口子（用户要求"后续增加不要架构大改动"）。

**备选**：直接走 trpc-agent-go 自带的 InMemorySession（被否：进程重启即丢，无法满足"会话历史"需求）。

### D5：历史窗口 + step 滚动清理双策略

**选择**：

- 短期上下文：`ai.session.history_window`（默认 20 条）控制送给模型的消息数。
- 长期容量：`ai.session.max_steps_size_mb`（默认 100MB，0 = 不清理）控制 `ai_agent_steps` 表大小。
- 清理只针对 `ai_agent_steps`，不影响用户消息（避免破坏对话连续性）。

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

## Risks / Trade-offs

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
