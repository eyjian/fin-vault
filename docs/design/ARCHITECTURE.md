# FinVault（锦仓）系统架构设计

> 文档版本：v1.0  
> 创建日期：2026-05-17  
> 状态：草稿  
> 基于：REQUIREMENT.md v1.0 + architecture-design.md v2.2

---

## 1. 设计目标

FinVault 的核心设计约束：**本地起步，升级改动最小**。

具体来说：

- 第一阶段以单机本地运行为主，SQLite 存储，进程内缓存，零外部依赖
- 后续商业化或分布式部署时，只需修改配置文件和注册新的基础设施实现，业务代码不需要改动
- 所有模块通过接口解耦，业务逻辑只依赖抽象，不依赖具体实现

---

## 2. 技术选型

### 2.1 后端技术栈（Go 1.21+）

| 用途 | 库 | 强约束 |
|------|-----|---------|
| Web 框架 | `gin-gonic/gin` | 不得换为 Hertz / GoFrame |
| 参数校验 | `go-playground/validator/v10` | Gin 自带 |
| ORM | `gorm.io/gorm` | 跨 SQLite/PG/MySQL/TDSQL 统一抽象 |
| SQLite 驱动 | `glebarez/sqlite` | **纯 Go，无 CGO**（关键：跨平台编译） |
| Redis | `redis/go-redis/v9` | 仅生产用，本地用 sync.Map+TTL |
| AI 客户端 | `trpc.group/trpc-go/trpc-agent-go` v1.9.x | **业务层不得直接 import**，只能经 `internal/llm` |
| 配置 | `spf13/viper` | YAML + 环境变量覆盖 |
| **金额** | **`shopspring/decimal`** | **禁止 float64/float32/int64 自造定点** |
| 日志 | 标准库 `slog` | 第一版结构化日志，未来可桥接 zap |
| 定时任务 | `robfig/cron/v3` | 理财到期扫描 |
| HTTP 客户端 | `go-resty/resty/v2` | 行情/平台 API 对接 |
| 协程池 | `panjf2000/ants/v2` | 行情批量拉取并发控制 |
| ID 生成 | `google/uuid`（UUID v7） | 自带时间序，**抽象为 IDGenerator** |
| 认证 | `golang-jwt/jwt/v5` | 多用户阶段启用 |
| Excel | `xuri/excelize/v2` | PDF 由前端浏览器打印 |
| 测试 | `stretchr/testify` + `DATA-DOG/go-sqlmock` | 不得引入其他测试框架 |

### 2.2 前端技术栈

Vue 3（用户确认）。UI 库待定，将在后续 ADR 中记录。

---

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

---

## 4. 核心抽象接口

> 📖 本章只描述各层之间的**抽象接口**。完整的领域模型实体定义、字段、索引、业务规则与事务边界，请参考 [domain-model.md](../domain-model.md)；建表 SQL 与 GORM Model 草稿请参考 [database-schema.md](../database-schema.md)。

**接口清单总览**（所有可能更换实现的地方都先抽象成接口，这是「升级改动最小」的核心保障）：

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

### 4.3 AI 适配层抽象（agent.Runner / model.NewDefaultModel / SessionStore）

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
type Runner interface {
    Run(ctx context.Context, sessionID string, userMessage string) (
        assistantMessage string,
        toolCalls []ToolCall,
        tokenUsage TokenUsage,
        err error,
    )
}
```

#### 抽象 2：SDK Model 工厂（model.NewDefaultModel）

```go
// internal/llm/model/factory.go

// NewDefaultModel 按 RegistryEntry 配置生成 OpenAI 兼容的 SDK Model。
func NewDefaultModel(cfg RegistryEntry, logger *slog.Logger) (sdkmodel.Model, string, error)
```

#### 抽象 3：会话持久化层（SessionStore）

```go
// internal/llm/session/store.go

type SessionStore interface {
    // 会话 CRUD
    CreateSession(ctx context.Context, s *domain.Session) error
    GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
    UpdateSession(ctx context.Context, s *domain.Session) error
    DeleteSession(ctx context.Context, sessionID string) error
    ListSessions(ctx context.Context, opts ListSessionsOptions) ([]domain.Session, int64, error)

    // 消息：按 created_at 升序拉取最近 N 条
    ListMessages(ctx context.Context, sessionID string, limit int) ([]domain.Message, error)
    AppendMessage(ctx context.Context, m *domain.Message) error

    // 步骤：仅追加从不更新；写入前 mask 敏感字段
    AppendStep(ctx context.Context, step *domain.AgentStep) error

    // EstimateStepsSize 估算 t_fv_ai_agent_steps 表占用空间（字节）
    EstimateStepsSize(ctx context.Context) (int64, error)
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

### 4.5 Migrator 抽象（数据库迁移）

```go
// internal/database/migrator.go

type Migrator interface {
    Up(ctx context.Context) error
    Down(ctx context.Context, steps int) error
    Version(ctx context.Context) (uint, bool, error)
}
```

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

### 4.7 EventBus 抽象（事件层，预留）

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

---

## 5. 项目目录结构

```
fin-vault/
├── docs/                           # 项目文档
│   ├── design/                     # 架构设计文档（本目录）
│   │   ├── ARCHITECTURE.md        # 本文档
│   │   └── data-interfaces.yaml    # 接口契约定义
│   ├── domain-model.md             # 领域模型设计
│   ├── database-schema.md          # 建表 SQL & GORM Model
│   └── upgrade-guide.md           # 升级改造指南
├── backend/                        # Go 后端
│   ├── cmd/
│   │   └── server/
│   │       └── main.go             # 入口：读取配置 → 调 bootstrap.Wire → 启动服务
│   ├── internal/
│   │   ├── bootstrap/              # 依赖组装（未来可改为 Wire ProviderSet）
│   │   │   ├── wire.go             # 集中初始化 DB / Cache / LLM / Repo / Svc / Handler
│   │   │   └── wire_ai.go         # AI 装配（含 D16 降级路径）
│   │   ├── config/                 # Viper 配置管理
│   │   │   └── config.go           # 配置结构体定义
│   │   ├── domain/                 # 领域模型（纯结构体，零外部依赖）
│   │   │   ├── holding.go          # 持仓
│   │   │   ├── transaction.go      # 交易记录
│   │   │   ├── asset.go            # 资产
│   │   │   └── ...                # 其他领域模型
│   │   ├── repository/             # 存储抽象
│   │   │   ├── interfaces.go       # Repository 接口定义（不引入 GORM）
│   │   │   └── gorm/               # GORM 实现
│   │   │       ├── holding_repo.go
│   │   │       └── ...
│   │   ├── service/                # 业务逻辑（只依赖接口；AI 编排也写在这里）
│   │   │   ├── asset_service.go    # 资产管理
│   │   │   ├── holding_service.go  # 持仓管理
│   │   │   ├── transaction_service.go # 交易管理
│   │   │   ├── ai_session_service.go # AI 会话管理
│   │   │   └── ai_message_service.go # AI 消息管理
│   │   ├── llm/                    # AI 适配层（唯一的 trpc-agent-go SDK 引用点）
│   │   │   ├── agent/              # 业务 Runner 接口 + trpc-agent-go 实装
│   │   │   ├── model/              # SDK Model 工厂
│   │   │   ├── session/            # 持久化层
│   │   │   └── tools/              # AI 工具 + ctx 隔离 helper
│   │   ├── cache/                  # 缓存抽象
│   │   │   ├── interfaces.go       # CacheProvider 接口
│   │   │   └── local.go            # 进程内缓存（本地用，sync.Map + TTL）
│   │   ├── event/                  # 事件抽象
│   │   │   └── interfaces.go       # EventBus 接口
│   │   ├── id/                     # ID 生成抽象
│   │   │   └── generator.go        # IDGenerator 接口
│   │   ├── report/                 # 报表导出抽象
│   │   │   └── exporter.go         # ReportExporter 接口
│   │   ├── handler/                # HTTP Handler（Gin）
│   │   │   ├── asset_handler.go    # 资产管理 API
│   │   │   ├── holding_handler.go  # 持仓管理 API
│   │   │   ├── transaction_handler.go # 交易管理 API
│   │   │   ├── ai_session_handler.go # AI 会话 API
│   │   │   └── ai_message_handler.go # AI 消息 API
│   │   ├── middleware/             # Gin 中间件
│   │   │   ├── auth.go             # 认证（JWT）
│   │   │   ├── cors.go             # 跨域
│   │   │   └── logger.go           # 请求日志（slog）
│   │   └── database/               # 数据库管理
│   │       ├── factory.go          # 数据库工厂（切换驱动）
│   │       └── automigrate.go      # GORM AutoMigrate 实现（第一阶段）
│   ├── pkg/                        # 可导出工具包
│   │   └── utils/
│   │       ├── response.go         # 统一响应结构
│   │       └── decimal.go          # 金额精度工具
│   ├── configs/                    # 配置文件
│   │   ├── local.yaml              # 本地开发配置
│   │   └── prod.yaml               # 生产配置模板
│   ├── go.mod
│   └── go.sum
├── frontend/                        # Vue3 前端
│   └── (待创建)
├── README.md
└── REQUIREMENT.md
```

---

## 6. 接口契约定义

接口契约详见 `data-interfaces.yaml`。

---

## 7. 配置管理

### 7.1 配置加载策略

使用 `spf13/viper` 管理配置，加载顺序（后者覆盖前者）：

1. `configs/config.yaml`（默认）
2. `configs/config.{env}.yaml`（环境差异）
3. 环境变量（`FV_` 前缀，下划线对应嵌套）

```go
v := viper.New()
v.SetConfigFile("configs/config.yaml")
_ = v.ReadInConfig()
if env := os.Getenv("FV_ENV"); env != "" {
    v.SetConfigFile(fmt.Sprintf("configs/config.%s.yaml", env))
    _ = v.MergeInConfig()
}
v.SetEnvPrefix("FV")
v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
v.AutomaticEnv()

var cfg Config
if err := v.Unmarshal(&cfg); err != nil { return nil, err }
```

### 7.2 敏感信息处理

- API Key 等敏感信息优先使用环境变量（如 `FV_LLM_DEEPSEEK_KEY`）
- 配置文件中可引用环境变量：`api_key: "${FV_LLM_DEEPSEEK_KEY}"`
- 生产环境建议使用 secrets 管理工具（如 Vault、K8s Secrets）

---

## 8. 依赖组装方式

所有依赖在 `internal/bootstrap/` 中通过显式组装（手动依赖注入），第一阶段不使用 DI 框架。`main.go` 只负责加载配置 + 调 `bootstrap.Wire(cfg)` + 启动 HTTP 服务：

```go
// cmd/server/main.go
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
        User:        gormrepo.NewUserRepository(db),
        Asset:        gormrepo.NewAssetRepository(db),
        Holding:      gormrepo.NewHoldingRepository(db),
        Transaction:  gormrepo.NewTransactionRepository(db),
        // ...
    }

    // 3. AI 装配（拆到 wire_ai.go，含 D16 降级路径）
    sessionStore := session.NewSQLiteStore(db, cfg.AI.Session.HistoryWindow)
    aiSessionH, aiMessageH := wireAI(cfg, repos, sessionStore, slog.Default())

    // 4. Services + Handlers 装配
    // ...

    return &App{
        Cfg: cfg, DB: db, Cache: cacheProv, Repos: repos,
        Handlers: &Handlers{
            Asset: ..., Holding: ..., Transaction: ...,
            AISession: aiSessionH,
            AIMessage: aiMessageH,
        },
    }, nil
}
```

**未来升级到 Wire**：把 `bootstrap/wire.go` 拆为多个 ProviderSet，用 `wire.Build(...)` 自动生成 `wire_gen.go` 即可，业务代码零改动。

---

## 9. 测试策略

### 9.1 单元测试

- **Repository 层**：使用 `DATA-DOG/go-sqlmock` 模拟数据库，零真实 DB 依赖
- **Service 层**：使用 mock 仓储（gomock 或手写），零 DB 依赖
- **Handler 层**：使用 `gin.CreateTestContext` + `httptest.NewRecorder` 测试 HTTP 接口

### 9.2 集成测试

- 使用 SQLite in-memory 数据库
- 测试关键业务场景（买入、卖出、持仓计算）
- 测试事务边界和并发控制

### 9.3 AI 测试

- Mock `agent.Runner` 接口，测试 Service 层的 AI 编排逻辑
- 测试 tool 调用的参数校验和返回值处理
- 测试 D13 用户隔离和 D14 message ID 关联

---

## 10. 部署策略

### 10.1 第一阶段（本地）

- 单机部署，SQLite 数据库
- 进程内缓存（sync.Map + TTL）
- 无需外部依赖

### 10.2 第二阶段（生产）

- 多实例部署，PostgreSQL/MySQL 数据库
- Redis 缓存
- 使用 Docker / K8s 部署

---

## 11. 未来扩展预留

### 11.1 微信小程序

前端独立开发，复用后端 API。需要增加：
- WeChat 登录认证中间件
- 小程序专用的 API 版本（`/api/v1/mini/`）

### 11.2 多用户 / SaaS

需要增加：
- 用户体系（注册、登录、JWT）
- 数据隔离（Repository 层加 user_id 过滤）
- 配额与计费
- 管理后台

### 11.3 分布式部署

参见 [upgrade-guide.md](../upgrade-guide.md)。

---

## 12. 附录：AI 层设计决策

### D12 物理隔离边界

`internal/llm/{agent,model,tools,session}/` 四子包是 SDK 仅有的导入点，service / handler / domain / repository 层 0 SDK 命中。

### D13 工具用户隔离

工具入参 schema **禁止**含 `user_id` 字段；service 层调 Runner 前 `tools.WithUserID(ctx, uid)` 注入 ctx，工具内部强制 `tools.UserIDFromContext` 提取。

### D14 step ↔ assistant message 关联

service 层预生成 `assistantMessageID = uuid.NewString()`，通过 `agent.WithAssistantMessageID(ctx, msgID)` 注入；Runner 落 assistant message 与 AgentStep 时使用同一 ID。

### D15 X-User-Id 强校验

AI 路由专用 `requireUserIDFromHeader`，缺失/非法/0 → 401。

### D16 LLM 不可用降级

`model.NewDefaultModel` 返 error 时仅装 `AISessionHandler`（CRUD 不依赖 Runner），`AIMessageHandler` 不装；`router.go` 条件挂载 `POST /ai/sessions/:id/messages`。

### D17 同步路径无需 flush

`appendStepSafe` 同步落库无缓冲，HTTP server 10s graceful shutdown 已能让正在执行的 Run() 完成最后一次同步 AppendStep。

---

## 13. 需求变更：资产列表页面展示持仓和盈亏数据

### 13.1 变更概述

**需求**：在资产列表页面（`/stock`、`/wealth`、`/cash`）展示持仓和盈亏数据。

**实现方式**：
- 后端扩展 Asset API (`GET /assets`)，新增可选查询参数 `?include_holdings=true`
- 前端更新三个页面，调用新的 API 参数，展示完整的持仓和盈亏数据

### 13.2 架构影响分析

#### 后端变更

| 组件 | 变更内容 | 影响范围 |
|------|---------|---------|
| `AssetService.List` | 添加 `includeHoldings` 参数，当为 true 时预加载持仓数据 | 小 |
| `AssetHandler.List` | 解析 `include_holdings` 查询参数，传递给 Service | 小 |
| `HoldingService` | 已有 `GetHoldingView` 方法，可直接复用 | 无（复用现有逻辑） |
| `HoldingView` | 已有完整持仓视图模型，包含所需所有字段 | 无 |

**关键发现**：
- 后端已有完整的持仓计算逻辑：`HoldingView` 模型包含所需所有字段
- `backend/internal/service/holding_service.go` 已有 `GetHoldingView` 方法
- 只需扩展 `AssetService.List` 方法，调用现有的 `HoldingService` 即可

#### 前端变更

| 组件 | 变更内容 | 影响范围 |
|------|---------|---------|
| `api/asset.ts` | 在 `getAssetList` 函数中添加 `include_holdings` 参数 | 小 |
| `StockManage.vue` | 更新表格列定义，添加持仓和盈亏相关列 | 中 |
| `WealthManage.vue` | 更新表格列定义，添加持仓和盈亏相关列 | 中 |
| `CashManage.vue` | 更新表格列定义，添加持仓和盈亏相关列 | 中 |

#### 性能考虑

- **不带 `include_holdings` 参数时**：API 响应时间 < 100ms（现有行为不变）
- **带 `include_holdings=true` 时**：需要查询持仓数据和计算盈亏，响应时间可能增加到 < 500ms
- **优化建议**：可以考虑批量查询持仓数据，避免 N+1 查询问题

#### API 兼容性

- 新增参数为可选，不影响现有 API 调用方
- 现有客户端不传递 `include_holdings` 参数时，API 行为完全不变
- 向前兼容，无破坏性变更

### 13.3 实现方案

#### 后端实现步骤

1. **修改 `AssetService.List` 方法**：
   ```go
   func (s *AssetService) List(ctx context.Context, opts ListOptions) ([]*domain.Asset, int64, error) {
       // 现有逻辑：查询资产列表
       
       // 新增：如果 includeHoldings 为 true，预加载持仓数据
       if opts.IncludeHoldings {
           for i, asset := range assets {
               holdingView, err := s.holdingService.GetHoldingView(ctx, asset.ID)
               if err != nil {
                   // 记录日志，继续处理其他资产
                   continue
               }
               // 将 holdingView 数据合并到 asset 响应中
               assets[i].HoldingView = holdingView
           }
       }
       
       return assets, total, nil
   }
   ```

2. **修改 `AssetHandler.List` 方法**：
   ```go
   func (h *AssetHandler) List(c *gin.Context) {
       // 解析 include_holdings 查询参数
       includeHoldings := c.Query("include_holdings") == "true"
       
       // 构建查询选项
       opts := service.ListOptions{
           IncludeHoldings: includeHoldings,
           // ... 其他现有选项
       }
       
       // 调用 Service 层
       assets, total, err := h.svc.List(c.Request.Context(), opts)
       // ... 后续处理
   }
   ```

3. **更新响应 DTO**：
   ```go
   type AssetResponse struct {
       // ... 现有字段
       
       // 持仓和盈亏数据（仅当 include_holdings=true 时返回）
       HoldingQuantity     *decimal.Decimal `json:"holding_quantity,omitempty"`
       HoldingAvgCost      *decimal.Decimal `json:"holding_avg_cost,omitempty"`
       HoldingLatestPrice *decimal.Decimal `json:"holding_latest_price,omitempty"`
       HoldingMarketValue *decimal.Decimal `json:"holding_market_value,omitempty"`
       HoldingUnrealizedPnl *decimal.Decimal `json:"holding_unrealized_pnl,omitempty"`
       HoldingTotalPnl    *decimal.Decimal `json:"holding_total_pnl,omitempty"`
       HoldingPnlRatio    *decimal.Decimal `json:"holding_pnl_ratio,omitempty"`
       HoldingRealizedPnl *decimal.Decimal `json:"holding_realized_pnl,omitempty"`
       HoldingTotalDividend *decimal.Decimal `json:"holding_total_dividend,omitempty"`
   }
   ```

#### 前端实现步骤

1. **修改 API 调用**：
   ```typescript
   // frontend/src/api/asset.ts
   export function getAssetList(params: {
     asset_type?: string;
     include_holdings?: boolean;
     // ... 其他参数
   }) {
     return request({
       url: '/api/v1/assets',
       method: 'get',
       params
     })
   }
   ```

2. **更新页面组件**：
   - 在 `fetchData` 时传递 `include_holdings=true`
   - 修改表格列定义，添加持仓和盈亏相关列
   - 使用合适的格式化（如绿色/红色显示盈亏）

### 13.4 测试策略

#### 后端测试

- **单元测试**：测试 `AssetService.List` 方法带/不带 `include_holdings` 参数的行为
- **集成测试**：测试完整的 API 调用链路，验证响应数据格式

#### 前端测试

- **单元测试**：测试 API 调用函数是否正确传递参数
- **组件测试**：测试表格是否正确展示持仓和盈亏数据

### 13.5 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| API 响应时间增加 | 用户体验 | 1. 参数为可选，不影响现有调用<br>2. 考虑添加缓存机制<br>3. 批量查询优化 |
| 前端页面渲染性能 | 用户体验 | 1. 虚拟滚动（大列表）<br>2. 分页加载 |
| 数据一致性 | 数据准确性 | 1. 使用事务保证一致性<br>2. 添加数据校验逻辑 |

## 14. 总结

本架构设计遵循「本地起步，升级改动最小」的核心原则，通过：

1. **接口抽象**：所有可能更换的实现都先抽象成接口
2. **物理隔离**：AI SDK 仅出现在 `internal/llm/` 下 4 子包
3. **集中装配**：所有依赖在 `internal/bootstrap/` 中组装
4. **零外部依赖**：本地阶段无需 Redis、PostgreSQL 等外部服务

确保第一阶段快速交付，后续升级时业务代码零改动或最小改动。

**新需求（资产列表页面展示持仓和盈亏数据）的架构影响**：
- 后端变更小，主要扩展 `AssetService.List` 方法
- 前端变更中等，需要更新三个页面组件
- API 完全向前兼容，无破坏性变更
- 利用现有 `HoldingService` 和 `HoldingView` 逻辑，避免重复开发
