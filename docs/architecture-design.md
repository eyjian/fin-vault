# FinVault（锦仓）架构设计规范

> 文档版本：v2.2（trpc-agent-go 替换定稿）  
> 创建日期：2026-05-16  
> 最近更新：2026-05-17  
> 状态：已确认

> v2.2 变更摘要（议题 `replace-ai-with-trpc-agent-go`）：
> - AI 层第一阶段直接采用 `trpc-group/trpc-agent-go` v1.9.x，**不再**自建 LLMProvider 接口或基于 `go-openai` 实现；业务代码 0 SDK 命中，物理隔离边界 D12（仅 `internal/llm/{agent,model,tools,session}/` 4 子包 import SDK）
> - 新增 3 张 AI 表（`t_fv_ai_sessions` / `t_fv_ai_messages` 新版 / `t_fv_ai_agent_steps`）替换原 `t_fv_ai_conversations` + `t_fv_ai_messages` 旧表
> - 新增设计决策档：D12 物理隔离 / D13 双 ctx 注入 / D14 step.MessageID 关联 / D15 X-User-Id 强校验 / D16 LLM 不可用降级 / D17 同步路径无需 flush
> - 7 个 AI 工具按反射 schema 自动生成；新增工具零侵入（一个文件 + bootstrap 装配 1 行）
> - 启动日志 5 条契约：`ai providers loaded` / `llm provider selected` / `llm tools registered` / `ai session config` / `ai endpoints status`

> v2.1 变更摘要（已被 v2.2 部分覆盖，保留作为演进记录）：
> - DB 迁移第一阶段只用 GORM AutoMigrate，接 PostgreSQL 时再引入 `golang-migrate`
> - ID 生成用 `google/uuid`（UUID v7，自带时间序），不再引入 snowflake，留接口预留
> - 报表导出第一阶段只装 `excelize/v2`，PDF 用前端浏览器打印，`gopdf` 推迟
> - DI 第一阶段不引入 Wire，初始化代码集中在 `internal/bootstrap/` 包，未来改造成本低

## 配套文档

- [domain-model.md](./domain-model.md)：领域模型详细设计（实体定义、业务规则、索引、事务边界）
- [database-schema.md](./database-schema.md)：数据库建表 SQL 与 GORM Model 草稿
- [upgrade-guide.md](./upgrade-guide.md)：本地→分布式升级路径与改造检查表

## 1. 设计目标

FinVault 的核心设计约束：**本地起步，升级改动最小**。

具体来说：

- 第一阶段以单机本地运行为主，SQLite 存储，进程内缓存，零外部依赖
- 后续商业化或分布式部署时，只需修改配置文件和注册新的基础设施实现，业务代码不需要改动
- 所有模块通过接口解耦，业务逻辑只依赖抽象，不依赖具体实现

## 2. 技术选型（v2.1 红队复盘定稿）

### 2.1 第一阶段依赖清单（13 个核心库）

| 层次 | 组件 | 选型 | 选型理由 |
|------|------|------|---------|
| 前端 | 框架 | Vue3 | 用户确认，生态成熟，组件库丰富 |
| 后端 | 语言 | Go 1.21+ | 用户确认，高性能、编译部署简单，标准库 `slog` 可用 |
| 后端 | Web 框架 | `gin-gonic/gin` | Go 生态最成熟，中间件丰富，社区最大 |
| 后端 | 参数校验 | `go-playground/validator/v10` | Gin 默认集成，无替代 |
| 后端 | ORM | `gorm.io/gorm` | 原生支持 SQLite/MySQL/PostgreSQL，切换数据库只需改驱动和 DSN |
| 后端 | SQLite 驱动 | `glebarez/sqlite` | 纯 Go 驱动，无 CGO，跨平台编译方便 |
| 后端 | 缓存客户端 | `redis/go-redis/v9` | Go 社区标准 Redis 客户端，支持哨兵/集群 |
| 后端 | AI 客户端 | `trpc.group/trpc-go/trpc-agent-go` | 腾讯 trpc 团队主推的 Agent 运行时（v1.9.x），内置 Session / Memory / Tool / Runner 四件套；OpenAI 兼容协议覆盖 DeepSeek / GLM / Kimi / 通义千问 / Ollama 等 90% 国内外模型 |
| 后端 | 配置管理 | `spf13/viper` | 支持多格式配置，环境变量覆盖，管理多模型 API Key |
| 后端 | 金额精度 | `shopspring/decimal` | 精确的十进制计算，避免浮点数精度问题 |
| 后端 | 日志 | `slog`（标准库） | 结构化日志，零依赖，生产环境可桥接到 zap |
| 后端 | 定时任务 | `robfig/cron/v3` | 成熟稳定的定时任务库 |
| 后端 | HTTP 客户端 | `go-resty/resty/v2` | 行情/平台 API 对接，自带重试/超时/Header/JSON 反序列化 |
| 后端 | 协程池 | `panjf2000/ants/v2` | 行情批量拉取与 AI 调用的并发控制 |
| 后端 | ID 生成 | `google/uuid`（UUID v7） | RFC 9562 标准，自带时间序，无需额外引入 snowflake |
| 后端 | 认证 | `golang-jwt/jwt/v5` | 商业化必备，本地阶段也用得上 |
| 后端 | Excel 导出 | `xuri/excelize/v2` | Go 生态事实标准 |
| 后端 | 测试 | `stretchr/testify` + `DATA-DOG/go-sqlmock` | Go 测试事实标准 |
| 数据库（本地） | SQLite | `glebarez/sqlite` | 见上 |
| 数据库（生产） | PostgreSQL/MySQL/TDSQL | GORM 对应驱动 | GORM 统一抽象，切换无代码改动 |

### 2.2 暂不引入（推迟或不再引入）

| 组件 | 原计划 | v2.1 决定 | 触发引入条件 |
|------|-------|----------|-------------|
| `cloudwego/eino` | 候选 Agent 框架 | **不选** | trpc-agent-go 已 v1.9.x 稳定可用；Eino 待转正 RC 后可作为 `agent.Runner` 的另一实现备选 |
| `tmc/langchaingo` | 候选 Agent 框架 | **不引入** | 维护节奏弱于 trpc-agent-go |
| `sashabaranov/go-openai` | 自研 Provider 客户端 | **不引入** | trpc-agent-go SDK 已封装 OpenAI 兼容协议，业务代码 0 SDK 命中 |
| `golang-migrate/migrate` | 第一阶段引入 | **推迟** | 接入 PostgreSQL / MySQL / TDSQL 时一并引入 |
| `bwmarrin/snowflake` | 预留分布式 ID | **不引入** | 由 UUID v7 + 接口预留 `IDGenerator` 替代，分布式时才考虑 |
| `signintech/gopdf` | PDF 报表导出 | **推迟** | 第一阶段 PDF 用前端 jsPDF / 浏览器打印解决 |
| `google/wire` | 编译期 DI | **推迟** | 当 `internal/bootstrap/` 初始化链超过 30 个组件时再上 Wire |

### 2.3 为什么不选其他方案

| 备选 | 不选原因 |
|------|---------|
| GoFrame 全家桶 | ORM 自有生态不兼容 GORM，AI 模块缺失，商业化换组件受框架约束 |
| Hertz（字节） | 社区规模远小于 Gin，参考资料少，外部生态不成熟 |
| 各厂商 Go Agent SDK | 厂商锁定，与「支持不同大模型」需求冲突 |
| Eino 当下作为 AI 主框架 | 仍在 alpha（v0.9.0-alpha.x），API 不稳定；trpc-agent-go 已 v1.9.x 稳定，文档/社区/迭代更优 |
| 自研 OpenAI 协议客户端 | trpc-agent-go SDK 已封装 OpenAI 兼容协议 + Tool Calling + Session/Memory；自研维护成本明显更高 |

### 2.4 AI 层落地决策（重要）

本项目 AI 能力是核心需求（基金搜索、行情查询、持仓分析、买卖建议、报表生成等），第一阶段直接采用 **`trpc-group/trpc-agent-go` v1.9.x** 作为 Agent 运行时，通过物理隔离 + 接口抽象保证「业务代码 0 SDK 命中」。

**真实业务需要的能力**（已 ✅ / 不需要 ❌）：

- ✅ 多模型 Provider 切换（DeepSeek / GLM / Kimi / 通义千问 / Ollama）—— 全部走 OpenAI 兼容协议，仅 BaseURL/APIKey/Model 不同
- ✅ Tool Calling（基金搜索、行情查询、持仓查询、盈亏计算等 7 个工具）—— SDK 内置反射生成 schema
- ✅ 多轮会话 + 历史窗口（默认 20 条）—— 业务 SessionStore 持久化到 SQLite，每次 Run 灌进 SDK inmemory session
- ✅ Agent 步骤可观测（tool_call_started / tool_call_finished / token_usage / step_boundary 四类事件落库）
- ✅ 步骤滚动清理（max_steps_size_mb 阈值，按 created_at 升序删旧）
- ✅ 跨用户隔离（D13 工具层强制 user_id ctx 注入）
- ❌ 流式 SSE 推送（首版返回完整响应，下个议题再加）
- ❌ RAG / 向量库（议题之外）
- ❌ Multi-Agent 协作 / 复杂 Graph 编排（议题之外）

**关键设计决策**（详见 [openspec design 档](../openspec/changes/replace-ai-with-trpc-agent-go/design.md)）：

- **D12 物理隔离边界**：`internal/llm/{agent,model,tools,session}/` 四子包是 SDK 仅有的导入点，service / handler / domain / repository 层 0 SDK 命中（通过 `grep` 红线把关）
- **D13 工具用户隔离**：工具入参 schema **禁止**含 `user_id` 字段；service 层调 Runner 前 `tools.WithUserID(ctx, uid)` 注入 ctx，工具内部强制 `tools.UserIDFromContext` 提取
- **D14 step ↔ assistant message 关联**：service 层预生成 `assistantMessageID = uuid.NewString()`，通过 `agent.WithAssistantMessageID(ctx, msgID)` 注入；Runner 落 assistant message 与 AgentStep 时使用同一 ID，保证 `step.MessageID == message.ID` 可追溯
- **D15 X-User-Id 强校验**：AI 路由专用 `requireUserIDFromHeader`，缺失/非法/0 → 401（不走普通业务路由的 fallback=1）
- **D16 LLM 不可用降级**：`model.NewDefaultModel` 返 error 时仅装 `AISessionHandler`（CRUD 不依赖 Runner），`AIMessageHandler` 不装；`router.go` 条件挂载 `POST /ai/sessions/:id/messages`（404 而非 500）
- **D17 同步路径无需 flush**：`appendStepSafe` 同步落库无缓冲，HTTP server 10s graceful shutdown 已能让正在执行的 Run() 完成最后一次同步 AppendStep；如未来 Runner 引入异步缓冲再补 flush 接口

**关键场景落地路径**：

- 新建会话 → `POST /api/v1/ai/sessions`（service 层 UUID 生成）
- 多轮对话 → `POST /api/v1/ai/sessions/:id/messages`（service 层先 Get 校验归属 → 双 ctx + assistant msg id 三注入 → Runner.Run）
- 历史消息 → `GET /api/v1/ai/sessions/:id/messages`（仅 user/assistant，tool 中间消息由 step 接口提供）
- 多模型切换 → `cfg.LLM.default` + `cfg.LLM.providers.<name>.{api_key,base_url,model,enabled}`，启动期 `model.NewDefaultModel` 选 default 或字典序 fallback
- Provider 元信息 → `GET /api/v1/ai/providers` 列出 name / model / is_default / enabled

**未来升级**：换 Agent 框架（如 Eino 转正）只需在 `internal/llm/agent/` 内换 `Runner` 实现 + `internal/llm/model/` 换 SDK 工厂，业务侧零改动。

## 3. 分层架构

```
┌──────────────────────────────────────────────────────┐
│                    Vue3 前端                          │
├──────────────────────────────────────────────────────┤
│               API Gateway (Gin Router)               │
├───────────┬───────────┬────────────┬────────────────┤
│  资产领域  │  分析领域  │  AI 领域   │   系统领域      │
│  Asset    │  Analysis │  AI Svc    │   System       │
├───────────┴───────────┴────────────┴────────────────┤
│      抽象层（接口定义，零外部依赖，升级零改动）            │
│  Repository │ CacheProvider │ agent.Runner │ EventBus│
│  IDGenerator│ Migrator      │ ReportExporter         │
│  SessionStore (持久化层)                              │
├───────────┬───────────┬────────────┬────────────────┤
│   GORM    │  go-redis │ trpc-agent │  (预留)        │
│  SQLite   │   本地    │   -go SDK  │  NATS/Kafka   │
│  PgSQL    │   单机    │  DeepSeek  │   Eino(后备)  │
│  MySQL    │   集群    │ GLM/Kimi/  │  (按需扩展)    │
│           │           │ Qwen/Ollama│                │
└───────────┴───────────┴────────────┴────────────────┘
```

### 3.1 各层职责

| 层 | 职责 | 依赖方向 |
|----|------|---------|
| Handler 层 | HTTP 请求处理、参数校验、响应序列化、X-User-Id 强校验（AI 路由） | 依赖 Service 接口；**0 SDK 命中** |
| Service 层 | 业务逻辑编排、事务管理、AI 路由前置 ctx 三注入（D13/D14） | 依赖 Repository / Cache / `agent.Runner` 接口；**0 SDK 命中** |
| Repository 层 | 数据持久化，数据库 CRUD | 依赖 domain 模型，实现 Repository 接口 |
| AI 适配层 | `internal/llm/` 下 4 子包，封装 trpc-agent-go SDK | 仅本层 import SDK，对外暴露 `agent.Runner` / `model.NewDefaultModel` / `session.SessionStore` 三个接口 |
| Domain 层 | 纯领域模型，结构体定义 | 零外部依赖 |

### 3.2 依赖规则

- **domain/** 零外部依赖，只有纯 Go 结构体
- **repository/interfaces.go** 定义接口，不引入 GORM
- **service/** 只依赖接口（repository / cache / `agent.Runner` / event 等抽象），AI 编排逻辑写在 service 层
- **handler/** 只依赖 service，不直接操作数据库、缓存或 SDK
- **`internal/llm/{agent,model,tools,session}/`** 是 trpc-agent-go SDK 的**唯一引用点**，业务层（service / handler / domain / repository）禁止 import SDK——CI 红线 `grep -rn "trpc.group/trpc-go/trpc-agent-go" backend/internal/{service,handler,domain,repository}/` 必须 0 命中
- **bootstrap/** 是装配层，允许 import SDK 子包（仅用于装配，不写业务逻辑）
- 具体实现（`gorm/`、`redis/`、`llm/agent/runner_trpc.go`）通过 `internal/bootstrap/` 包的工厂函数注入，`main.go` 调用 `bootstrap.Wire()` 完成组装

## 4. 核心抽象接口

> 📖 本章只描述各层之间的**抽象接口**。完整的领域模型实体定义、字段、索引、业务规则与事务边界，请参考 [domain-model.md](./domain-model.md)；建表 SQL 与 GORM Model 草稿请参考 [database-schema.md](./database-schema.md)。

**v2.1 接口清单总览**（所有可能更换实现的地方都先抽象成接口，这是「升级改动最小」的核心保障）：

| 接口 | 第一阶段实现 | 未来扩展实现 | 触发升级条件 |
|------|------------|-------------|-------------|
| `Repository`（各领域） | `repository/gorm/*` | 无变化（GORM 跨 DB 无差异） | 接 PostgreSQL / MySQL / TDSQL 时只改 DSN |
| `CacheProvider` | `cache/local.go`（进程内 sync.Map + TTL） | `cache/redis.go` | 多实例部署或需要持久化缓存 |
| `agent.Runner` | `llm/agent/runner_trpc.go`（trpc-agent-go SDK 实现） | `llm/agent/runner_eino.go` 等 | 换 Agent 框架（Eino 转正等）；业务侧零改动 |
| `session.SessionStore` | `llm/session/sqlite_store.go`（SQLite 持久化 3 张 AI 表） | `llm/session/mysql_store.go` 等 | 接生产数据库时只换工厂选择 |
| `EventBus` | `event/channel.go`（Go channel） | `event/nats.go` / `event/kafka.go` | 跨实例事件广播 |
| `IDGenerator` | `id/uuidv7.go`（google/uuid） | `id/snowflake.go` | 分布式部署 + 需要更短/纯数字 ID |
| `Migrator` | `database/automigrate.go`（GORM AutoMigrate） | `database/golang_migrate.go`（双向 SQL） | 接入生产数据库（PostgreSQL/MySQL/TDSQL） |
| `ReportExporter` | `report/excel.go`（excelize/v2） | `report/pdf.go`（gopdf 或 wkhtmltopdf） | 用户明确要求服务端生成 PDF |

### 4.1 Repository 抽象（存储层）

```go
// repository/interfaces.go

// 通用 CRUD 接口，各领域 Repository 嵌入此接口
type BaseRepository[T any] interface {
    Create(ctx context.Context, entity *T) error
    GetByID(ctx context.Context, id uint) (*T, error)
    Update(ctx context.Context, entity *T) error
    Delete(ctx context.Context, id uint) error
    List(ctx context.Context, opts ListOptions) ([]T, int64, error)
}

// 持仓仓储
type HoldingRepository interface {
    BaseRepository[Holding]
    ListByPlatform(ctx context.Context, platform string) ([]Holding, error)
    ListByType(ctx context.Context, assetType AssetType) ([]Holding, error)
    GetSummary(ctx context.Context) (*HoldingSummary, error)
}

// 交易记录仓储
type TransactionRepository interface {
    BaseRepository[Transaction]
    ListByHolding(ctx context.Context, holdingID uint) ([]Transaction, error)
    ListByDateRange(ctx context.Context, start, end time.Time) ([]Transaction, error)
}

// 报表仓储
type ReportRepository interface {
    BaseRepository[Report]
    GetByTypeAndPeriod(ctx context.Context, reportType ReportType, period string) (*Report, error)
}
```

数据库切换方式——只改工厂函数和配置：

```go
// internal/database/factory.go

func NewDB(cfg *config.DatabaseConfig) *gorm.DB {
    switch cfg.Driver {
    case "sqlite":
        return openSQLite(cfg.DSN)
    case "postgres":
        return openPostgres(cfg.DSN)
    case "mysql", "tdsql":
        return openMySQL(cfg.DSN)
    default:
        panic(fmt.Sprintf("unsupported database driver: %s", cfg.Driver))
    }
}
```

### 4.2 CacheProvider 抽象（缓存层）

```go
// cache/interfaces.go

type CacheProvider interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
}
```

本地阶段实现——进程内缓存，无需 Redis：

```go
// cache/local.go

type localCache struct {
    store sync.Map
    // 带 TTL 的简单实现，使用 time.AfterFunc 清理过期键
}
```

商业化阶段实现——Redis：

```go
// cache/redis.go

type redisCache struct {
    client *redis.Client
}
```

切换方式——只改工厂函数：

```go
// cache/factory.go

func NewCacheProvider(cfg *config.CacheConfig) CacheProvider {
    switch cfg.Driver {
    case "local":
        return newLocalCache()
    case "redis":
        return newRedisCache(cfg.Redis)
    default:
        return newLocalCache()
    }
}
```

### 4.3 AI 适配层抽象（agent.Runner / model.NewDefaultModel / SessionStore）

第一阶段实现策略：**`internal/llm/{agent,model,tools,session}/` 4 子包封装 trpc-agent-go SDK，业务代码（service / handler / domain / repository）只 import 这 4 个子包，永远不直接 import SDK**。CI 红线 `grep -rn "trpc.group/trpc-go/trpc-agent-go" backend/internal/{service,handler,domain,repository}/` 必须 0 命中。

#### 抽象 1：业务 Runner 接口（agent.Runner）

```go
// internal/llm/agent/runner.go

// ToolCall 描述 Agent 在一次 turn 内调用过的一个工具的可观测信息。
type ToolCall struct {
    Name         string                 `json:"name"`
    Arguments    map[string]interface{} `json:"arguments"`
    StartedAt    time.Time              `json:"started_at"`
    FinishedAt   time.Time              `json:"finished_at"`
    Status       string                 `json:"status"` // success / failed / timeout
    ErrorMessage string                 `json:"error_message,omitempty"`
}

// TokenUsage 对应 spec ai-agent-runtime "token 用量被记录" Scenario 的载荷。
type TokenUsage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}

// Runner 业务侧 Agent 运行时接口。
//
// 业务 Runner 自管 user/assistant 两条消息落库（D12 第 5 步）+ AgentStep
// 落库（含 D14 message_id 关联），service 层不重复写。
type Runner interface {
    Run(ctx context.Context, sessionID string, userMessage string) (
        assistantMessage string,
        toolCalls []ToolCall,
        tokenUsage TokenUsage,
        err error,
    )
}
```

实现位于 `agent/runner_trpc.go`，`bootstrap/wire_ai.go` 通过 `agent.NewTRPCRunner(factory, store, historyWindow, logger)` 构造单例。换 Agent 框架（如 Eino 转正）只需新增 `runner_eino.go` 实现 `Runner`，业务侧零改动。

#### 抽象 2：SDK Model 工厂（model.NewDefaultModel）

```go
// internal/llm/model/factory.go

// NewDefaultModel 按 RegistryEntry 配置生成 OpenAI 兼容的 SDK Model。
//
// 选择策略：
//  1. cfg.Default 非空且可用 → 直接使用，info 记 "selected (default)"
//  2. 否则按 providers map keys 字典序遍历，取第一个可用的，warn 记 "fallback by dictionary order"
//  3. 全部不可用 → 返回非 nil error（D16 LLM 不可用降级触发点）
//
// 可用 = Enabled && APIKey != "" && BaseURL != "" && Model != ""
func NewDefaultModel(cfg RegistryEntry, logger *slog.Logger) (sdkmodel.Model, string, error)
```

`RegistryEntry` / `ProviderEntry` 是工厂输入版本（纯 Go 字段）；mapstructure 反序列化版本是 `RegistryConfig` / `ProviderConfig`（含 `*bool Enabled` 三态 + `IsEnabled()` 方法），bootstrap 通过 `cfg.LLM.ToRegistryEntry()` 桥接两者。

#### 抽象 3：会话持久化层（SessionStore）

```go
// internal/llm/session/store.go

type SessionStore interface {
    // 会话 CRUD（用户隔离由 service 层完成；store 层是受信调用）
    CreateSession(ctx context.Context, s *domain.Session) error
    GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
    UpdateSession(ctx context.Context, s *domain.Session) error
    DeleteSession(ctx context.Context, sessionID string) error
    ListSessions(ctx context.Context, opts ListSessionsOptions) ([]domain.Session, int64, error)

    // 消息：按 created_at 升序拉取最近 N 条（spec "历史窗口生效"）
    ListMessages(ctx context.Context, sessionID string, limit int) ([]domain.Message, error)
    AppendMessage(ctx context.Context, m *domain.Message) error

    // 步骤：仅追加从不更新；写入前 mask 敏感字段
    AppendStep(ctx context.Context, step *domain.AgentStep) error

    // EstimateStepsSize 估算 t_fv_ai_agent_steps 表占用空间（字节），
    // 用于 ai.session.max_steps_size_mb 滚动清理
    EstimateStepsSize(ctx context.Context) (int64, error)
}
```

第一阶段实现 `sqlite_store.go`（基于 GORM AutoMigrate 的 3 张 AI 表）；未来接 PostgreSQL/MySQL 时只新增对应实现，business 层零感知。`SessionStore` 上方还可叠 `Cache` 接口（默认 `NoopCache`），后续接 Redis 时只换实现。

#### 多模型路由（无独立 Registry 类型）

trpc-agent-go SDK 已内置 Model 抽象，FinVault **不**自建 LLMRegistry。多模型路由由 `model.NewDefaultModel` 一次性选定（启动期），运行时不再做 per-request 的 provider 切换；如未来需要"按请求参数路由不同 provider"，可在 `agent.Runner` 实现内分发到多个 SDK Model 实例（每个 cfg.Providers entry 各构造一个）。

Provider 元信息暴露给前端：`AIMetaHandler` 直接消费 `cfg.LLM`（`model.RegistryConfig`），按字典序输出 `[]ProviderInfo{name, model, is_default, enabled}`，对应 `GET /api/v1/ai/providers`。

#### Service 层 ctx 三注入（D13 + D14）

```go
// internal/service/ai_message_service.go (节选)
func (s *AIMessageService) Send(ctx context.Context, userID uint, sessionID, userMsg string) (*SendResult, error) {
    // 1. 归属校验先于 Runner.Run
    sess, err := s.sessionSvc.Get(ctx, userID, sessionID)
    if err != nil { return nil, err }

    // 2. 双 ctx + D14 第三注入
    ctx = tools.WithUserID(ctx, userID)                          // D13 工具层 user 隔离（uint）
    ctx = agent.WithUserID(ctx, fmt.Sprint(userID))              // D12 SDK session.Key 拼接（string）
    assistantMsgID := uuid.NewString()
    ctx = agent.WithAssistantMessageID(ctx, assistantMsgID)      // D14 step ↔ assistant message 关联

    // 3. Runner.Run（错误透传，含 50001/50004-50007）
    assistantText, toolCalls, usage, err := s.runner.Run(ctx, sess.ID, userMsg)
    // ...
}
```

### 4.4 IDGenerator 抽象（ID 生成）

```go
// internal/id/generator.go

type IDGenerator interface {
    NextID() string         // 字符串型 ID（UUID v7 / Snowflake 字符串）
    NextInt64() int64       // 数字型 ID（仅 Snowflake 实现支持，UUID 实现可返回错误）
}
```

第一阶段实现：

```go
// internal/id/uuidv7.go

type uuidV7Generator struct{}

func (g *uuidV7Generator) NextID() string {
    id, _ := uuid.NewV7()
    return id.String()
}
```

### 4.5 Migrator 抽象（数据库迁移）

```go
// internal/database/migrator.go

type Migrator interface {
    Up(ctx context.Context) error
    Down(ctx context.Context, steps int) error
    Version(ctx context.Context) (uint, bool, error)
}
```

第一阶段实现：`database/automigrate.go`，调用 `db.AutoMigrate(&models...)` 即可，配合 `migrations/` 目录手写 SQL 备份作为人工核对参考。

第二阶段（接 PostgreSQL / MySQL / TDSQL 时）：新增 `database/golang_migrate.go`，引入 `golang-migrate/migrate`，把 `migrations/` 改造为标准的 `001_xxx.up.sql` / `001_xxx.down.sql`。

### 4.6 ReportExporter 抽象（报表导出）

```go
// internal/report/exporter.go

type ReportFormat string

const (
    FormatExcel    ReportFormat = "xlsx"
    FormatMarkdown ReportFormat = "md"
    FormatPDF      ReportFormat = "pdf"   // 第一阶段不实现
)

type ReportExporter interface {
    Format() ReportFormat
    Export(ctx context.Context, report *domain.Report, w io.Writer) error
}
```

第一阶段只实现 `excel.go`（excelize/v2）和 `markdown.go`（标准库 text/template），PDF 由前端浏览器打印或 jsPDF 解决。

### 4.4 EventBus 抽象（事件层，预留）

```go
// event/interfaces.go

type Event struct {
    Topic   string
    Payload any
}

type EventHandler func(ctx context.Context, event Event) error

type EventPublisher interface {
    Publish(ctx context.Context, event Event) error
}

type EventSubscriber interface {
    Subscribe(topic string, handler EventHandler) error
}

// 组合接口
type EventBus interface {
    EventPublisher
    EventSubscriber
}
```

本地阶段实现——Go channel：

```go
// event/channel.go

type channelBus struct {
    subscribers map[string][]EventHandler
    ch         chan Event
}
```

分布式阶段实现——NATS / Kafka：

```go
// event/nats.go（未来实现）

type natsBus struct {
    conn *nats.Conn
}
```

## 5. 项目目录结构

```
fin-vault/
├── docs/                           # 项目文档
│   ├── architecture-design.md      # 本文档：架构设计规范
│   ├── domain-model.md             # 领域模型设计
│   ├── database-schema.md          # 建表 SQL & GORM Model
│   └── upgrade-guide.md            # 升级改造指南
├── backend/                        # Go 后端
│   ├── cmd/
│   │   └── server/
│   │       └── main.go             # 入口：读取配置 → 调 bootstrap.Wire → 启动服务
│   ├── internal/
│   │   ├── bootstrap/              # 依赖组装（未来可改为 Wire ProviderSet）
│   │   │   └── wire.go             # 集中初始化 DB / Cache / LLM / Repo / Svc / Handler
│   │   ├── config/                 # Viper 配置管理
│   │   │   ├── config.go           # 配置结构体定义
│   │   │   └── config_test.go
│   │   ├── domain/                 # 领域模型（纯结构体，零外部依赖）
│   │   │   ├── holding.go          # 持仓
│   │   │   ├── transaction.go      # 交易记录
│   │   │   ├── product.go          # 理财产品
│   │   │   ├── fund.go             # 基金
│   │   │   ├── stock.go            # 股票
│   │   │   └── report.go           # 报表
│   │   ├── repository/             # 存储抽象
│   │   │   ├── interfaces.go       # Repository 接口定义（不引入 GORM）
│   │   │   └── gorm/               # GORM 实现
│   │   │       ├── holding_repo.go
│   │   │       ├── transaction_repo.go
│   │   │       ├── product_repo.go
│   │   │       ├── fund_repo.go
│   │   │       ├── stock_repo.go
│   │   │       └── report_repo.go
│   │   ├── service/                # 业务逻辑（只依赖接口；AI 编排也写在这里）
│   │   │   ├── asset_service.go    # 资产管理
│   │   │   ├── analysis_service.go # 盈亏分析（含 Tool Calling 循环）
│   │   │   ├── advisor_service.go  # 买卖/持仓建议（含 Tool Calling 循环）
│   │   │   ├── report_service.go   # 报表生成（数据采集→AI 分析→模板渲染）
│   │   │   ├── chat_service.go     # 智能问答（流式 SSE）
│   │   │   └── sync_service.go     # 数据同步（行情、净值）
│   │   ├── llm/                    # AI 适配层（唯一的 trpc-agent-go SDK 引用点）
│   │   │   ├── agent/              # 业务 Runner 接口 + trpc-agent-go 实装
│   │   │   │   ├── runner.go               # agent.Runner 接口定义（业务侧）
│   │   │   │   ├── runner_trpc.go          # trpcRunner：SDK Runner 包装
│   │   │   │   ├── tools_registration.go   # NewToolsetAgentFactory：工具清单装配 + 启动日志
│   │   │   │   ├── event_handler.go        # SDK Event → AgentStep 落库（含掩码）
│   │   │   │   ├── error_mapping.go        # SDK error → 业务错误码（50001/50004-50007）
│   │   │   │   └── context.go              # WithUserID / WithAssistantMessageID（D13/D14 ctx 注入）
│   │   │   ├── model/              # SDK Model 工厂
│   │   │   │   ├── factory.go              # NewDefaultModel：default + 字典序 fallback
│   │   │   │   └── types.go                # RegistryEntry / ProviderEntry / RegistryConfig (mapstructure)
│   │   │   ├── session/            # 持久化层
│   │   │   │   ├── store.go                # SessionStore 接口
│   │   │   │   ├── sqlite_store.go         # SQLite 实现（3 张 AI 表）
│   │   │   │   ├── cache.go                # Cache 接口 + NoopCache（预留 Redis）
│   │   │   │   ├── cleanup.go              # max_steps_size_mb 滚动清理
│   │   │   │   └── mask.go                 # MaskSensitiveJSON（D7 写库前掩码）
│   │   │   └── tools/              # 7 个 AI 工具 + ctx 隔离 helper
│   │   │       ├── context.go              # WithUserID / UserIDFromContext (D13)
│   │   │       ├── search_fund.go          # 基金搜索（公共数据，无需 user_id）
│   │   │       ├── market_quote.go         # 行情快照（含用户绑定校验）
│   │   │       ├── market_data.go          # 行情数据查询
│   │   │       ├── holding_query.go        # 持仓查询（D13 强制 user_id 过滤）
│   │   │       ├── profit_calc.go          # 盈亏计算
│   │   │       ├── platform_summary.go     # 平台资产汇总
│   │   │       └── history_query.go        # 历史交易查询
│   │   ├── cache/                  # 缓存抽象
│   │   │   ├── interfaces.go       # CacheProvider 接口
│   │   │   ├── local.go            # 进程内缓存（本地用，sync.Map + TTL）
│   │   │   ├── redis.go            # Redis 缓存（生产用，未来启用）
│   │   │   └── factory.go          # 缓存工厂
│   │   ├── event/                  # 事件抽象
│   │   │   ├── interfaces.go       # EventBus 接口
│   │   │   └── channel.go          # Go channel 实现（本地用）
│   │   ├── id/                     # ID 生成抽象
│   │   │   ├── generator.go        # IDGenerator 接口
│   │   │   └── uuidv7.go           # UUID v7 实现（google/uuid）
│   │   ├── report/                 # 报表导出抽象
│   │   │   ├── exporter.go         # ReportExporter 接口
│   │   │   ├── excel.go            # Excel 导出（excelize/v2）
│   │   │   └── markdown.go         # Markdown 导出（text/template）
│   │   ├── handler/                # HTTP Handler（Gin）
│   │   │   ├── asset_handler.go    # 资产管理 API
│   │   │   ├── analysis_handler.go # 分析统计 API
│   │   │   ├── chat_handler.go     # AI 对话 API（SSE 流式）
│   │   │   ├── advisor_handler.go  # 买卖/持仓建议 API
│   │   │   └── report_handler.go   # 报表 API
│   │   ├── middleware/             # Gin 中间件
│   │   │   ├── auth.go             # 认证（JWT，第一阶段单用户也用得上）
│   │   │   ├── cors.go             # 跨域
│   │   │   └── logger.go           # 请求日志（slog）
│   │   └── database/               # 数据库管理
│   │       ├── factory.go          # 数据库工厂（切换驱动）
│   │       ├── sqlite.go           # SQLite 初始化
│   │       ├── postgres.go         # PostgreSQL 初始化（未来启用）
│   │       ├── mysql.go            # MySQL/TDSQL 初始化（未来启用）
│   │       ├── migrator.go         # Migrator 接口
│   │       └── automigrate.go      # GORM AutoMigrate 实现（第一阶段）
│   ├── pkg/                        # 可导出工具包
│   │   └── utils/
│   │       ├── encrypt.go          # AES 加解密（用于 API Key 等敏感信息）
│   │       └── response.go         # 统一响应结构
│   ├── migrations/                 # 数据库迁移脚本（人工核对，第二阶段交给 golang-migrate）
│   ├── configs/                    # 配置文件
│   │   ├── local.yaml              # 本地开发配置
│   │   └── prod.yaml               # 生产配置模板
│   ├── go.mod
│   └── go.sum
├── frontend/                        # Vue3 前端
│   └── (待创建)
├── README.md
└── LICENSE
```

> 关键改动点（vs v1.0）：
> - 新增 `internal/bootstrap/` 集中依赖组装，未来可改造为 Wire ProviderSet
> - 原 `agent/` 目录拆为 `llm/`（适配层）+ `service/*_service.go`（编排层）
> - 新增 `id/`、`report/` 抽象层
> - `database/` 拆出 `automigrate.go`，第二阶段并入 `golang_migrate.go`

## 6. 配置文件规范

### 6.1 本地开发配置（configs/local.yaml）

```yaml
server:
  port: 8080
  mode: debug    # debug / release

database:
  driver: sqlite
  dsn: "./data/fin-vault.db"

cache:
  driver: local    # 本地阶段用进程内缓存

llm:
  default: deepseek
  providers:
    deepseek:
      api_key: "sk-xxx"
      base_url: "https://api.deepseek.com"
      model: "deepseek-chat"
    glm:
      api_key: "xxx"
      base_url: "https://open.bigmodel.cn/api/paas/v4"
      model: "glm-4"

security:
  encryption_key: "本地加密密钥"

log:
  level: debug
  format: console
```

### 6.2 生产配置模板（configs/prod.yaml）

```yaml
server:
  port: 8080
  mode: release

database:
  driver: postgres
  dsn: "host=${DB_HOST} port=5432 user=${DB_USER} password=${DB_PASSWORD} dbname=finvault sslmode=disable"

cache:
  driver: redis
  redis:
    addr: "${REDIS_ADDR}"
    password: "${REDIS_PASSWORD}"
    db: 0

llm:
  default: glm
  providers:
    glm:
      api_key: "${GLM_API_KEY}"
      base_url: "https://open.bigmodel.cn/api/paas/v4"
      model: "glm-4"

security:
  encryption_key: "${ENCRYPTION_KEY}"

log:
  level: info
  format: json
```

## 7. AI Agent 能力设计

### 7.1 工具清单（7 个）

Agent 的核心能力通过 Tool 注册实现，每个 Tool 一个文件，入参定义为 Go 结构体 + `json` / `description` tag，运行时反射生成 JSON Schema（D6）。**工具入参 schema 禁止含 `user_id` 字段**（D13），用户身份由 service 层通过 `tools.WithUserID(ctx, uid)` 注入 ctx，工具内部强制 `tools.UserIDFromContext` 提取。

| 工具名称 | 功能 | 用户身份隔离 | 调用场景 |
|---------|------|-------------|---------|
| `search_fund` | 按 keyword 模糊匹配基金 code/name，结果上限 20 | 公共数据，**无需** user_id | 用户对话中模糊搜基金 |
| `market_quote` | 按 symbol（如 `sh000001`）查实时行情快照 | **需要** user_id（资产权属校验） | 上证指数、个股/基金当前价 |
| `market_data` | 行情数据查询（含历史） | **需要** user_id | 持仓分析、行情对比 |
| `holding_query` | 查询当前持仓明细 | **需要** user_id（强制 WHERE user_id=?） | 盈亏分析、持仓建议 |
| `profit_calc` | 计算盈亏数据 | **需要** user_id | 盈亏分析、报表生成 |
| `platform_summary` | 各平台资产汇总 | **需要** user_id | 全局视图、配置建议 |
| `history_query` | 查询历史交易记录 | **需要** user_id | 交易复盘、报表生成 |

### 7.2 工具装配范式（NewToolsetAgentFactory）

```go
// internal/bootstrap/wire_ai.go (节选)
aiTools := []sdktool.CallableTool{
    tools.NewSearchFundTool(tools.SearchFundDeps{Asset: repos.Asset}),
    tools.NewMarketQuoteTool(tools.MarketQuoteDeps{Quote: repos.Quote, Asset: repos.Asset}),
    tools.NewMarketDataTool(tools.MarketDataDeps{Quote: repos.Quote, Asset: repos.Asset}),
    tools.NewHoldingQueryTool(tools.HoldingQueryDeps{Holding: repos.Holding, Asset: repos.Asset}),
    tools.NewProfitCalcTool(tools.ProfitCalcDeps{Holding: repos.Holding, Quote: repos.Quote}),
    tools.NewPlatformSummaryTool(tools.PlatformSummaryDeps{
        Holding: repos.Holding, Platform: repos.Platform, Quote: repos.Quote,
    }),
    tools.NewHistoryQueryTool(tools.HistoryQueryDeps{Transaction: repos.Transaction}),
}

factory := agent.NewToolsetAgentFactory(
    agent.DefaultAppName, sdkModel, aiTools, logger, defaultAIInstruction,
    0, // 0 → DefaultMaxToolIterations=10
)
runner := agent.NewTRPCRunner(factory, sessionStore, cfg.AI.Session.HistoryWindow, logger)
```

工具构造**只依赖 repository 接口**，无 SDK 命中；启动期一次性构造后被 factory 持有，每次 Run 共享同一组工具实例（无状态）。新增工具的工程动作：

1. 在 `internal/llm/tools/` 新建 `<tool_name>.go`，定义 input struct + Run 函数 + `New<ToolName>Tool(deps)` 构造函数
2. 在 `bootstrap/wire_ai.go` 的 `buildAITools` 函数末尾追加一行
3. 单测覆盖：成功 + 失败 + D13 跨用户回归（`*_OtherUser_Returns404Like` 模板）

### 7.3 启动日志契约（5 条）

为方便运维巡检，bootstrap 装配 AI 时按顺序输出 5 条结构化日志：

| # | level | message | 关键字段 | 触发时机 |
|---|-------|---------|---------|---------|
| 1 | info | `ai providers loaded` | `configured` (排序后的 provider 名列表) / `default` | bootstrap 装配 AI 入口 |
| 2 | info/warn | `llm provider selected (default)` 或 `llm default provider unavailable, fallback by dictionary order` | `provider` / `model` | `model.NewDefaultModel` 选定 SDK Model |
| 3 | info | `llm tools registered` | `tools` (工具名清单) / `count` | `NewToolsetAgentFactory` 构造完毕 |
| 4 | info | `ai session config` | `history_window` / `max_steps_size_mb` | session 配置可观测 |
| 5 | info | `ai endpoints status` | `session_enabled` / `message_enabled` (+ 降级路径附 `reason`) | 装配最终状态 |

D16 降级路径下 #2 #3 自然不打（`NewDefaultModel` error 提前返回），#4 #5 仍输出，便于一眼看出降级状态：

```
WARN  AI message endpoint disabled (D16 degrade)  reason=...
INFO  ai session config                            history_window=20  max_steps_size_mb=100
INFO  ai endpoints status                          session_enabled=true  message_enabled=false  reason=...
```

### 7.4 SSE 流式对话（推迟）

第一版返回完整响应（`POST /api/v1/ai/sessions/:id/messages` 同步返回 assistant_message + tool_calls + token_usage），SSE 流式输出留待下个议题。trpc-agent-go SDK 已支持流式接口，扩展点位于 `agent/runner_trpc.go`。

## 8. 依赖组装方式

所有依赖在 `internal/bootstrap/` 中通过显式组装（手动依赖注入），第一阶段不使用 DI 框架。`main.go` 只负责加载配置 + 调 `bootstrap.Wire(cfg)` + 启动 HTTP 服务：

```go
// cmd/api/main.go
func main() {
    cfg, err := bootstrap.LoadConfig("configs/config.yaml")
    if err != nil { log.Fatal(err) }
    app, err := bootstrap.Wire(cfg)
    if err != nil { log.Fatal(err) }
    defer app.Close()
    r := bootstrap.RegisterRoutes(app)
    // ... HTTP server start with 10s graceful shutdown
}
```

```go
// internal/bootstrap/wire.go（核心装配，节选）

func Wire(cfg *Config) (*App, error) {
    // 1. DB / Cache
    db, _ := NewDB(cfg.Database)
    cacheProv := NewCache(cfg.Cache)

    // 2. Repositories（GORM 实现）
    repos := &repository.Repositories{
        UoW:         gormrepo.NewUnitOfWork(db),
        User:        gormrepo.NewUserRepository(db),
        // ... Asset / Holding / Transaction / Quote / Rate 等
    }

    // 3. AI 装配（拆到 wire_ai.go，含 D16 降级路径）
    sessionStore := session.NewSQLiteStore(db, cfg.AI.Session.HistoryWindow)
    aiSessionH, aiMessageH := wireAI(cfg, repos, sessionStore, slog.Default())

    // 4. PlatformAPI Aggregator（行情）+ Services + Handlers + Cron 装配
    // ... 略

    return &App{
        Cfg: cfg, DB: db, Cache: cacheProv, Repos: repos,
        Aggregator: aggregator, Cron: cm,
        Handlers: &Handlers{
            Meta: ..., Asset: ..., /* 业务 handlers */
            AISession: aiSessionH,
            AIMessage: aiMessageH, // D16 降级路径下为 nil
            AIMeta:    handler.NewAIMetaHandler(cfg.LLM),
        },
    }, nil
}
```

```go
// internal/bootstrap/router.go（条件挂载，节选）

func RegisterRoutes(app *App) *gin.Engine {
    r := gin.New()
    // ... 中间件链
    v1 := r.Group("/api/v1")
    h := app.Handlers
    h.Meta.Register(v1); h.Asset.Register(v1); /* ... 业务路由 */

    if h.AISession != nil { h.AISession.Register(v1) }
    if h.AIMessage != nil { h.AIMessage.Register(v1) } // D16: 降级时跳过 POST /messages
    if h.AIMeta    != nil { h.AIMeta.Register(v1) }
    return r
}
```

这种显式组装的好处：

- 依赖关系一目了然
- 替换任何组件只改这一处
- 不需要反射、不需要代码生成
- 编译期就能发现依赖错误
- AI 装配单独拆到 `wire_ai.go`，便于测试 + 隔离 D16 降级逻辑

**未来升级到 Wire**：把 `bootstrap/wire.go` 拆为多个 ProviderSet（`InfraSet` / `RepoSet` / `LLMSet` / `ServiceSet` / `HandlerSet`），用 `wire.Build(...)` 自动生成 `wire_gen.go` 即可，业务代码零改动。触发条件：`bootstrap/wire.go` 超过 200 行或组件数超过 30 个。

## 9. 未来扩展预留

### 9.1 微信小程序

前端独立开发，复用后端 API。需要增加：
- WeChat 登录认证中间件
- 小程序专用 API 版本（`/api/v1/mini/`）

### 9.2 多用户 / SaaS

需要增加：
- 用户体系（注册、登录、JWT）
- 数据隔离（Repository 层加 user_id 过滤）
- 配额与计费
- 管理后台

### 9.3 分布式部署

参见 [upgrade-guide.md](./upgrade-guide.md)。
