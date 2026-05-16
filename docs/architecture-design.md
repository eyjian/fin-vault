# FinVault（锦仓）架构设计规范

> 文档版本：v1.0  
> 创建日期：2026-05-16  
> 状态：已确认

## 1. 设计目标

FinVault 的核心设计约束：**本地起步，升级改动最小**。

具体来说：

- 第一阶段以单机本地运行为主，SQLite 存储，进程内缓存，零外部依赖
- 后续商业化或分布式部署时，只需修改配置文件和注册新的基础设施实现，业务代码不需要改动
- 所有模块通过接口解耦，业务逻辑只依赖抽象，不依赖具体实现

## 2. 技术选型（已确认）

| 层次 | 组件 | 选型 | 选型理由 |
|------|------|------|---------|
| 前端 | 框架 | Vue3 | 用户确认，生态成熟，组件库丰富 |
| 后端 | 语言 | Go | 用户确认，高性能、编译部署简单 |
| 后端 | Web 框架 | Gin | Go 生态最成熟，中间件丰富，社区最大 |
| 后端 | ORM | GORM | 原生支持 SQLite/MySQL/PostgreSQL，切换数据库只需改驱动和 DSN |
| 后端 | 缓存客户端 | go-redis | Go 社区标准 Redis 客户端，支持哨兵/集群 |
| 后端 | AI 框架 | langchaingo | Go 生态最成熟的 AI/Agent 框架，内置多模型适配 |
| 后端 | 配置管理 | Viper | 支持多格式配置，环境变量覆盖，管理多模型 API Key |
| 数据库（本地） | SQLite | GORM SQLite 驱动 | 单文件，零配置，本地运行最轻量 |
| 数据库（生产） | PostgreSQL/MySQL/TDSQL | GORM 对应驱动 | GORM 统一抽象，切换无代码改动 |

### 2.1 为什么不选其他方案

| 备选 | 不选原因 |
|------|---------|
| GoFrame 全家桶 | ORM 自有生态不兼容 GORM，AI 模块缺失，商业化换组件受框架约束 |
| Hertz（字节） | 社区规模远小于 Gin，参考资料少，外部生态不成熟 |
| go-openai SDK | 不是 Agent 框架，缺少 Tool/Chain/Memory 等能力 |
| 各厂商 Go Agent SDK | 厂商锁定，与"支持不同大模型"需求冲突 |

### 2.2 Go Agent 框架选型

本项目 AI 能力是核心需求（买卖建议、盈亏分析、报表生成等），需要 Agent 框架支撑。Go 生态中主要 Agent 框架对比如下：

| 框架 | 成熟度 | 模型支持 | Agent 能力 | 流式输出 | RAG | 社区 |
|------|--------|---------|-----------|---------|-----|------|
| **langchaingo** | ⭐⭐⭐⭐ | OpenAI/DeepSeek/GLM/Ollama/Anthropic 等 | ReAct Agent、Tool Calling、Chain、Memory | ✅ | ✅ 内置 | 活跃 |
| go-openai | ⭐⭐⭐⭐⭐ | 仅 OpenAI 兼容接口 | ❌ 无，纯 API 调用 | ✅ | ❌ | 官方维护 |
| 各大厂 Go Agent SDK | ⭐⭐ | 绑定特定厂商 | 有但受限 | 不确定 | 不确定 | 弱 |

**最终选择：langchaingo + 自建 Provider 抽象层**

选型理由：

1. **langchaingo 解决 80% 的基础能力**：统一的 `llms` 模型接口、`Chains` 链式调用、`Agents` 推理+工具调用、`Memory` 对话记忆、`Tools` 工具注册——这些 FinVault 都需要
2. **自建 Provider 抽象层解决 langchaingo 的不足**：langchaingo 的模型接入方式不够灵活，部分模型需要绕路。通过 `LLMProvider` 接口封装，业务代码不直接依赖 langchaingo，未来可替换底层框架而不影响业务
3. **关键场景落地路径清晰**：
   - 盈亏分析 → Agent + 注册财务数据查询 Tool
   - 买卖建议 → Agent + 行情数据 Tool + 市场分析 Prompt
   - 智能问答 → Chain + Memory + RAG（检索理财知识）
   - 周报/月报/年报 → Chain（数据采集→分析→模板渲染）
   - 多模型切换 → Provider 抽象层 + 配置热更新
4. **架构演进友好**：本地阶段 langchaingo 直连各模型 API；商业化阶段可引入消息队列解耦、多实例 Agent 调度；未来换框架只需替换 Provider 实现，业务代码不变

## 3. 分层架构

```
┌──────────────────────────────────────────────────────┐
│                    Vue3 前端                          │
├──────────────────────────────────────────────────────┤
│               API Gateway (Gin Router)               │
├───────────┬───────────┬────────────┬────────────────┤
│  资产领域  │  分析领域  │  AI 领域   │   系统领域      │
│  Asset    │  Analysis │  Agent     │   System       │
├───────────┴───────────┴────────────┴────────────────┤
│            抽象层（接口定义，零外部依赖）                │
│  Repository │ CacheProvider │ LLMProvider │ EventBus │
├───────────┬───────────┬────────────┬────────────────┤
│   GORM    │  go-redis │langchaingo │  (预留)        │
│  SQLite   │   本地    │  DeepSeek  │  NATS/Kafka   │
│  PgSQL    │   单机    │    GLM     │               │
│  MySQL    │   集群    │  Ollama    │               │
└───────────┴───────────┴────────────┴────────────────┘
```

### 3.1 各层职责

| 层 | 职责 | 依赖方向 |
|----|------|---------|
| Handler 层 | HTTP 请求处理、参数校验、响应序列化 | 依赖 Service 接口 |
| Service 层 | 业务逻辑编排，事务管理 | 依赖 Repository/Cache/LLM 接口 |
| Repository 层 | 数据持久化，数据库 CRUD | 依赖 domain 模型，实现 Repository 接口 |
| Agent 层 | AI 对话、工具调用、买卖建议 | 依赖 LLMProvider 接口和 Service 接口 |
| Domain 层 | 纯领域模型，结构体定义 | 零外部依赖 |

### 3.2 依赖规则

- **domain/** 零外部依赖，只有纯 Go 结构体
- **repository/interfaces.go** 定义接口，不引入 GORM
- **service/** 只依赖接口（repository interfaces、cache interfaces、llm interfaces）
- **handler/** 只依赖 service，不直接操作数据库或缓存
- 具体实现（gorm/、redis/、langchain/）通过工厂函数注入，main.go 中组装

## 4. 核心抽象接口

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

```go
// agent/provider.go

type LLMProvider interface {
    // 基础对话
    Chat(ctx context.Context, messages []Message) (*Response, error)
    // 流式对话（SSE 推送到前端）
    StreamChat(ctx context.Context, messages []Message) (<-chan Chunk, error)
    // Function Calling / Tool Use
    ChatWithTools(ctx context.Context, messages []Message, tools []Tool) (*Response, error)
}

// 消息结构
type Message struct {
    Role    string `json:"role"`    // system / user / assistant / tool
    Content string `json:"content"`
}

// 流式输出块
type Chunk struct {
    Content string `json:"content"`
    Done    bool   `json:"done"`
}
```

langchaingo 统一适配实现：

```go
// agent/langchain.go

type langchainProvider struct {
    llm llms.Model
}

// DeepSeek、GLM、Moonshot 等都走 OpenAI 兼容协议，只需改 baseURL
func NewLLMProvider(cfg *config.LLMConfig) LLMProvider {
    opts := []option.Option{
        option.WithToken(cfg.APIKey),
        option.WithModel(cfg.Model),
        option.WithBaseURL(cfg.BaseURL),
    }
    llm, _ := openai.New(opts...)
    return &langchainProvider{llm: llm}
}
```

多模型配置示例：

```go
// 支持运行时切换模型
type LLMRegistry struct {
    providers map[string]LLMProvider
    default_  string
}

func (r *LLMRegistry) GetProvider(name ...string) LLMProvider {
    key := r.default_
    if len(name) > 0 {
        key = name[0]
    }
    return r.providers[key]
}
```

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
│   └── upgrade-guide.md            # 升级改造指南
├── backend/                        # Go 后端
│   ├── cmd/
│   │   └── server/
│   │       └── main.go             # 入口：读取配置、组装依赖、启动服务
│   ├── internal/
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
│   │   │   ├── interfaces.go       # Repository 接口定义
│   │   │   └── gorm/              # GORM 实现
│   │   │       ├── holding_repo.go
│   │   │       ├── transaction_repo.go
│   │   │       ├── product_repo.go
│   │   │       ├── fund_repo.go
│   │   │       ├── stock_repo.go
│   │   │       └── report_repo.go
│   │   ├── service/                # 业务逻辑（只依赖接口）
│   │   │   ├── asset_service.go    # 资产管理
│   │   │   ├── analysis_service.go # 盈亏分析
│   │   │   ├── report_service.go   # 报表生成
│   │   │   └── sync_service.go     # 数据同步
│   │   ├── agent/                  # AI Agent 层
│   │   │   ├── provider.go         # LLMProvider 接口 + LLMRegistry
│   │   │   ├── langchain.go        # langchaingo 适配实现
│   │   │   ├── tools/              # Agent 工具注册
│   │   │   │   ├── market_data.go  # 行情查询工具
│   │   │   │   ├── holding_query.go# 持仓查询工具
│   │   │   │   └── profit_calc.go  # 盈亏计算工具
│   │   │   └── advisor/            # 智能顾问
│   │   │       ├── buy_sell.go     # 买卖建议 Agent
│   │   │       ├── holding.go      # 持仓建议 Agent
│   │   │       └── report.go       # 报表生成 Agent
│   │   ├── cache/                  # 缓存抽象
│   │   │   ├── interfaces.go       # CacheProvider 接口
│   │   │   ├── local.go            # 进程内缓存（本地用）
│   │   │   ├── redis.go            # Redis 缓存（生产用）
│   │   │   └── factory.go          # 缓存工厂
│   │   ├── event/                  # 事件抽象
│   │   │   ├── interfaces.go       # EventBus 接口
│   │   │   └── channel.go          # Go channel 实现（本地用）
│   │   ├── handler/                # HTTP Handler（Gin）
│   │   │   ├── asset_handler.go    # 资产管理 API
│   │   │   ├── analysis_handler.go # 分析统计 API
│   │   │   ├── agent_handler.go    # AI 对话 API（SSE 流式）
│   │   │   └── report_handler.go   # 报表 API
│   │   ├── middleware/              # Gin 中间件
│   │   │   ├── auth.go             # 认证
│   │   │   ├── cors.go             # 跨域
│   │   │   └── logger.go           # 请求日志
│   │   └── database/               # 数据库管理
│   │       ├── factory.go          # 数据库工厂（切换驱动）
│   │       ├── sqlite.go           # SQLite 初始化
│   │       ├── postgres.go         # PostgreSQL 初始化
│   │       ├── mysql.go            # MySQL/TDSQL 初始化
│   │       └── migrator.go         # 自动迁移
│   ├── pkg/                        # 可导出工具包
│   │   └── utils/
│   │       ├── encrypt.go          # AES 加解密
│   │       └── response.go         # 统一响应结构
│   ├── migrations/                  # 数据库迁移脚本
│   ├── configs/                     # 配置文件
│   │   ├── local.yaml              # 本地开发配置
│   │   └── prod.yaml               # 生产配置模板
│   ├── go.mod
│   └── go.sum
├── frontend/                        # Vue3 前端
│   └── (待创建)
├── README.md
└── LICENSE
```

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
前端 EventSource → Gin SSE Handler → LLMProvider.StreamChat() → langchaingo 流式调用
```

Gin SSE 实现要点：
- 设置 `Content-Type: text/event-stream`
- 设置 `Cache-Control: no-cache` 和 `Connection: keep-alive`
- 使用 `c.Stream()` 方法持续推送 Chunk

## 8. 依赖组装方式

所有依赖在 `main.go` 中通过显式组装（手动依赖注入），不使用 DI 框架：

```go
func main() {
    // 1. 加载配置
    cfg := config.Load("configs/local.yaml")

    // 2. 初始化基础设施
    db := database.NewDB(&cfg.Database)
    cache := cache.NewCacheProvider(&cfg.Cache)
    llmRegistry := agent.NewLLMRegistry(&cfg.LLM)
    eventBus := event.NewChannelBus()

    // 3. 初始化 Repository（注入 DB）
    holdingRepo := gormRepo.NewHoldingRepo(db)
    transactionRepo := gormRepo.NewTransactionRepo(db)
    reportRepo := gormRepo.NewReportRepo(db)

    // 4. 初始化 Service（注入 Repository + Cache）
    assetSvc := service.NewAssetService(holdingRepo, transactionRepo, cache)
    analysisSvc := service.NewAnalysisService(holdingRepo, transactionRepo, cache)
    reportSvc := service.NewReportService(reportRepo, holdingRepo, cache)

    // 5. 初始化 Agent（注入 LLMProvider + Service）
    advisor := advisor.NewBuySellAdvisor(llmRegistry.GetProvider(), assetSvc, analysisSvc)

    // 6. 初始化 Handler（注入 Service + Agent）
    assetHandler := handler.NewAssetHandler(assetSvc)
    analysisHandler := handler.NewAnalysisHandler(analysisSvc)
    agentHandler := handler.NewAgentHandler(llmRegistry, advisor)

    // 7. 启动 Gin
    r := gin.Default()
    registerRoutes(r, assetHandler, analysisHandler, agentHandler)
    r.Run(fmt.Sprintf(":%d", cfg.Server.Port))
}
```

这种显式组装的好处：
- 依赖关系一目了然
- 替换任何组件只改这一处
- 不需要反射、不需要代码生成
- 编译期就能发现依赖错误

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
