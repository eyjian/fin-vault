# FinVault（锦仓）架构设计规范

> 文档版本：v2.1（红队复盘定稿）  
> 创建日期：2026-05-16  
> 最近更新：2026-05-16  
> 状态：已确认

> v2.1 变更摘要：
> - AI 层第一阶段不自建 Agent 框架，直接用 `go-openai` 原生 Tool Calling，仅预留 `LLMProvider` 接口；Eino/langchaingo 推迟到出现复杂多步推理需求时再引入
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
| 后端 | AI 客户端 | `sashabaranov/go-openai` | OpenAI 协议事实标准，原生支持 Tool Calling / 流式 / JSON Mode，覆盖 DeepSeek / GLM / Kimi / 通义千问 / Ollama 等 90% 国内外模型 |
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
| `cloudwego/eino` | 自建薄 Agent 层 | **暂不引入** | 当真的出现「需要多步推理 + 工具循环 + 复杂编排」的 Agent 场景（例如复杂报表生成 Agent） |
| `tmc/langchaingo` | 候选 Agent 框架 | **不引入** | 已被 Eino 替代为后续选项 |
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
| 第一阶段就上 Eino | YAGNI 原则——`go-openai` 已原生支持 Tool Calling / Streaming / JSON Mode，第一阶段真实需要的能力（Provider 切换 + Tool Calling + 流式输出 + 简单 Memory）全部覆盖；不需要 Multi-Agent / 复杂 Graph 编排 / RAG |
| 第一阶段就上 langchaingo | 同上，且 langchaingo 维护节奏弱于 Eino，未来若要升级会优先选 Eino |

### 2.4 AI 层落地决策（重要）

本项目 AI 能力是核心需求（买卖建议、盈亏分析、报表生成等），但**第一阶段不自建 Agent 框架，也不引入 Eino / langchaingo**。

**真实业务需要的能力**（已 ✅ / 不需要 ❌）：

- ✅ 多模型 Provider 切换（DeepSeek / GLM / Kimi / 通义千问 / Ollama）
- ✅ Tool Calling（查持仓、查行情、查历史交易）—— `go-openai` 原生支持
- ✅ 流式输出 SSE 推送给前端 —— `go-openai` 原生支持
- ✅ 简单 Memory（最近 N 轮对话）—— 业务层自管理 `[]Message` 即可
- ❌ Multi-Agent 协作
- ❌ 复杂 Graph / DAG 编排
- ❌ RAG（理财知识量不大，直接 Prompt 塞进上下文够用）

**结论**：

1. 第一阶段 AI 层**只定义 `LLMProvider` 接口 + `OpenAIProvider` 实现**（基于 `go-openai`）
2. 业务层直接调用 `provider.Chat(ctx, messages, tools)`，不再包一层「薄 Agent」
3. 多步推理/工具循环逻辑由业务 Service 层显式编写（最多就 ReAct 几十行代码）
4. 待出现复杂 Agent 场景时，再新增 `EinoProvider` 实现 `LLMProvider` 接口，业务代码不动

**关键场景落地路径**：

- 盈亏分析 → `Service` 层调用 `Provider.ChatWithTools(...)` + 注册财务数据查询 Tool
- 买卖建议 → `Service` 层调用 `Provider.ChatWithTools(...)` + 行情数据 Tool + 市场分析 Prompt
- 智能问答 → `Service` 层维护 `[]Message` 上下文 + 调用 `Provider.StreamChat(...)`
- 周报/月报/年报 → 显式三步 Pipeline：数据采集（Repository）→ 分析（Provider.Chat）→ 模板渲染（excelize/Markdown）
- 多模型切换 → `LLMRegistry.GetProvider(name)` 按配置/请求参数路由

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
│  Repository │ CacheProvider │ LLMProvider │ EventBus │
│  IDGenerator│ Migrator      │ ReportExporter         │
├───────────┬───────────┬────────────┬────────────────┤
│   GORM    │  go-redis │ go-openai  │  (预留)        │
│  SQLite   │   本地    │  DeepSeek  │  NATS/Kafka   │
│  PgSQL    │   单机    │    GLM     │  Eino/Anthropic│
│  MySQL    │   集群    │  Ollama    │  (按需扩展)    │
└───────────┴───────────┴────────────┴────────────────┘
```

### 3.1 各层职责

| 层 | 职责 | 依赖方向 |
|----|------|---------|
| Handler 层 | HTTP 请求处理、参数校验、响应序列化 | 依赖 Service 接口 |
| Service 层 | 业务逻辑编排、事务管理、AI 调用编排（含 Tool Calling 循环） | 依赖 Repository / Cache / LLMProvider 接口 |
| Repository 层 | 数据持久化，数据库 CRUD | 依赖 domain 模型，实现 Repository 接口 |
| AI 适配层 | `LLMProvider` 接口 + `OpenAIProvider` 实现 + Tool 注册表 | 依赖 `go-openai`，对外只暴露 `LLMProvider` 接口 |
| Domain 层 | 纯领域模型，结构体定义 | 零外部依赖 |

### 3.2 依赖规则

- **domain/** 零外部依赖，只有纯 Go 结构体
- **repository/interfaces.go** 定义接口，不引入 GORM
- **service/** 只依赖接口（repository / cache / llm / event 等抽象），AI 编排逻辑写在 service 层
- **handler/** 只依赖 service，不直接操作数据库、缓存或 LLM
- **llm/openai.go** 是 `go-openai` 的唯一引用点，业务层不得直接 import `go-openai`
- 具体实现（`gorm/`、`redis/`、`llm/openai/`）通过 `internal/bootstrap/` 包的工厂函数注入，`main.go` 调用 `bootstrap.Wire()` 完成组装

## 4. 核心抽象接口

> 📖 本章只描述各层之间的**抽象接口**。完整的领域模型实体定义、字段、索引、业务规则与事务边界，请参考 [domain-model.md](./domain-model.md)；建表 SQL 与 GORM Model 草稿请参考 [database-schema.md](./database-schema.md)。

**v2.1 接口清单总览**（所有可能更换实现的地方都先抽象成接口，这是「升级改动最小」的核心保障）：

| 接口 | 第一阶段实现 | 未来扩展实现 | 触发升级条件 |
|------|------------|-------------|-------------|
| `Repository`（各领域） | `repository/gorm/*` | 无变化（GORM 跨 DB 无差异） | 接 PostgreSQL / MySQL / TDSQL 时只改 DSN |
| `CacheProvider` | `cache/local.go`（进程内 sync.Map + TTL） | `cache/redis.go` | 多实例部署或需要持久化缓存 |
| `LLMProvider` | `llm/openai.go`（基于 go-openai） | `llm/eino.go` / `llm/anthropic.go` | 出现复杂 Multi-Agent / Graph 编排需求 |
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

### 4.3 LLMProvider 抽象（AI 模型层）

第一阶段实现策略：**只定义接口 + 一个基于 `go-openai` 的实现**，业务代码只 import `internal/llm`，永远不直接 import `go-openai`。

```go
// internal/llm/provider.go

type LLMProvider interface {
    // 基础对话（一次性返回完整回复）
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    // 流式对话（SSE 推送到前端）
    StreamChat(ctx context.Context, req ChatRequest) (<-chan Chunk, error)
    // 带 Tool Calling 的对话（业务层在 Service 中循环调用直到 finish_reason=stop）
    ChatWithTools(ctx context.Context, req ChatRequest, tools []Tool) (*ChatResponse, error)
    // Embeddings（未来 RAG 用，第一阶段可不实现）
    Embeddings(ctx context.Context, input []string) ([][]float32, error)
}

// 请求参数
type ChatRequest struct {
    Model       string    // 可空，空则用 Provider 默认模型
    Messages    []Message
    Temperature float32
    MaxTokens   int
    JSONMode    bool      // 强制返回 JSON
}

// 消息结构（兼容 OpenAI 协议）
type Message struct {
    Role       string     `json:"role"`    // system / user / assistant / tool
    Content    string     `json:"content"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    Name       string     `json:"name,omitempty"`
}

type Tool struct {
    Name        string
    Description string
    Parameters  any        // JSON Schema
    Handler     func(ctx context.Context, args string) (string, error)
}

type ToolCall struct {
    ID        string
    Name      string
    Arguments string
}

type ChatResponse struct {
    Content      string
    ToolCalls    []ToolCall
    FinishReason string     // stop / tool_calls / length
    Usage        TokenUsage
}

type Chunk struct {
    Content string `json:"content"`
    Done    bool   `json:"done"`
}

type TokenUsage struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
}
```

基于 `go-openai` 的统一适配实现（覆盖 DeepSeek / GLM / Kimi / 通义千问 / Ollama 等所有 OpenAI 协议兼容模型）：

```go
// internal/llm/openai.go

type openaiProvider struct {
    client       *openai.Client
    defaultModel string
}

// DeepSeek、GLM、Kimi、通义千问、Ollama 等都走 OpenAI 兼容协议，只需改 baseURL
func NewOpenAIProvider(cfg *config.LLMProviderConfig) LLMProvider {
    oc := openai.DefaultConfig(cfg.APIKey)
    if cfg.BaseURL != "" {
        oc.BaseURL = cfg.BaseURL
    }
    return &openaiProvider{
        client:       openai.NewClientWithConfig(oc),
        defaultModel: cfg.Model,
    }
}
```

多模型注册与运行时路由：

```go
// internal/llm/registry.go

type LLMRegistry struct {
    providers map[string]LLMProvider
    fallback  string
}

// 默认 Provider；调用方也可显式指定 name
func (r *LLMRegistry) GetProvider(name ...string) LLMProvider {
    key := r.fallback
    if len(name) > 0 && name[0] != "" {
        key = name[0]
    }
    return r.providers[key]
}
```

> 业务层 Tool Calling 循环示例（写在 Service 层，不在 llm 层）：
>
> ```go
> // internal/service/advisor_service.go（伪代码）
> for {
>     resp, _ := s.llm.ChatWithTools(ctx, req, tools)
>     if resp.FinishReason != "tool_calls" { return resp.Content, nil }
>     for _, tc := range resp.ToolCalls {
>         result, _ := tools[tc.Name].Handler(ctx, tc.Arguments)
>         req.Messages = append(req.Messages, llm.Message{
>             Role: "tool", ToolCallID: tc.ID, Content: result,
>         })
>     }
> }
> ```

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
│   │   ├── llm/                    # AI 适配层（唯一的 go-openai 引用点）
│   │   │   ├── provider.go         # LLMProvider 接口、Message、Tool、Chunk 等类型
│   │   │   ├── openai.go           # 基于 go-openai 的 OpenAIProvider 实现
│   │   │   ├── registry.go         # LLMRegistry：多模型注册与运行时路由
│   │   │   └── tools/              # AI Tool 注册（供 Service 层挑选注册）
│   │   │       ├── market_data.go  # 行情查询工具
│   │   │       ├── holding_query.go# 持仓查询工具
│   │   │       ├── profit_calc.go  # 盈亏计算工具
│   │   │       ├── history_query.go# 历史交易查询工具
│   │   │       └── platform_summary.go # 平台资产汇总工具
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

### 7.1 Agent 工具注册

Agent 的核心能力通过 Tool 注册实现，每个 Tool 对应一个具体的数据查询或计算能力：

| 工具名称 | 功能 | Agent 调用场景 |
|---------|------|---------------|
| market_data | 查询股票/基金实时行情 | 买卖建议、持仓分析 |
| holding_query | 查询当前持仓明细 | 盈亏分析、持仓建议 |
| profit_calc | 计算盈亏数据 | 盈亏分析、报表生成 |
| history_query | 查询历史交易记录 | 交易复盘、报表生成 |
| platform_summary | 各平台资产汇总 | 全局视图、配置建议 |

### 7.2 Agent 场景映射

| 用户场景 | 实现方式 | 依赖的工具 |
|---------|---------|-----------|
| 持仓分析 | ReAct Agent + 财务数据 Tool | holding_query, profit_calc, market_data |
| 买卖建议 | ReAct Agent + 行情 Tool + 分析 Prompt | market_data, holding_query |
| 智能问答 | Chain + Memory + RAG | 全部工具 |
| 周报/月报/年报 | Chain（数据采集→分析→模板渲染） | holding_query, profit_calc, history_query |
| 持仓建议 | ReAct Agent + 配置分析 Prompt | platform_summary, market_data |

### 7.3 SSE 流式对话

AI 对话场景必须支持流式输出，实现方案：

```
前端 EventSource → Gin SSE Handler → ChatService → LLMProvider.StreamChat() → go-openai 流式调用
```

Gin SSE 实现要点：
- 设置 `Content-Type: text/event-stream`
- 设置 `Cache-Control: no-cache` 和 `Connection: keep-alive`
- 使用 `c.Stream()` 方法持续推送 Chunk

## 8. 依赖组装方式

所有依赖在 `internal/bootstrap/wire.go` 中通过显式组装（手动依赖注入），第一阶段不使用 DI 框架。`main.go` 只负责加载配置 + 调 `bootstrap.Wire(cfg)` + 启动 HTTP 服务：

```go
// cmd/server/main.go
func main() {
    cfg := config.Load("configs/local.yaml")
    app, cleanup, err := bootstrap.Wire(cfg)
    if err != nil { log.Fatal(err) }
    defer cleanup()
    app.Run()
}
```

```go
// internal/bootstrap/wire.go（第一阶段手写，未来改造为 Wire ProviderSet）

func Wire(cfg *config.Config) (*App, func(), error) {
    // 1. 基础设施
    db := database.NewDB(&cfg.Database)
    migrator := database.NewAutoMigrator(db)
    _ = migrator.Up(context.Background())

    cacheProvider := cache.NewCacheProvider(&cfg.Cache)
    llmRegistry := llm.NewRegistry(&cfg.LLM)
    eventBus := event.NewChannelBus()
    idGen := id.NewUUIDv7Generator()

    // 2. Repository（注入 DB）
    holdingRepo := gormRepo.NewHoldingRepo(db)
    transactionRepo := gormRepo.NewTransactionRepo(db)
    reportRepo := gormRepo.NewReportRepo(db)

    // 3. AI Tool 注册（依赖 Repo，供 Service 挑选）
    toolRegistry := tools.NewRegistry(holdingRepo, transactionRepo /* ... */)

    // 4. Service（注入 Repository + Cache + LLM + Tools）
    assetSvc := service.NewAssetService(holdingRepo, transactionRepo, cacheProvider, idGen)
    analysisSvc := service.NewAnalysisService(holdingRepo, transactionRepo, cacheProvider, llmRegistry, toolRegistry)
    advisorSvc := service.NewAdvisorService(holdingRepo, llmRegistry, toolRegistry)
    reportSvc := service.NewReportService(reportRepo, holdingRepo, llmRegistry, excelExporter, mdExporter)
    chatSvc := service.NewChatService(llmRegistry, toolRegistry)

    // 5. Handler（注入 Service）
    h := handler.New(assetSvc, analysisSvc, advisorSvc, reportSvc, chatSvc)

    // 6. 路由
    r := gin.Default()
    h.RegisterRoutes(r)

    cleanup := func() { /* 关闭 db / cache / eventbus 等 */ }
    return &App{Engine: r, Cfg: cfg}, cleanup, nil
}
```

这种显式组装的好处：
- 依赖关系一目了然
- 替换任何组件只改这一处
- 不需要反射、不需要代码生成
- 编译期就能发现依赖错误

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
