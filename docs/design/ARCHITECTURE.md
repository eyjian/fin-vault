# FinVault（锦仓）落地架构（架构师定稿）

> 版本：v1.0-impl · 出品：架构师 陈架构 · 创建：2026-05-16
>
> 本文是「可落地施工图」，与 `docs/architecture-design.md`（v2.1，决策定稿）、`docs/domain-model.md`、`docs/database-schema.md` 三件套**对齐且不冲突**：
> - 三件套是「设计依据」（决策、技术选型、领域模型、SQL）
> - 本文是「施工蓝图」（最终目录结构、模块依赖、构造与中间件顺序、关键时序）
>
> **施工方依据本文 + naming-conventions + go-gin-development 两个 workspace skill 即可开干。**

---

## 1. 核心约束（不可妥协）

| # | 约束 | 落地点 |
|---|------|--------|
| 1 | 业务层只依赖接口，不直接 import gorm/redis/openai | `internal/service/**` 不得出现 `gorm.io` / `redis/go-redis` / `sashabaranov/go-openai` |
| 2 | 五层目录铁律 | `cmd/api/main.go` + `internal/{domain,repository,service,handler,llm,bootstrap}` + `pkg/{errs,utils}` |
| 3 | 依赖单向向内 | `handler → service → repository(接口) → gorm 实现` |
| 4 | 配置只读一次 | `viper` 仅在 `bootstrap.LoadConfig` 调用 |
| 5 | 中间件顺序 | `RequestID → Logger → Recovery → CORS → Auth(可选) → 业务` |
| 6 | 事务边界在 Service | 通过 `repository.UnitOfWork` 接口下沉到 GORM 实现 |
| 7 | 金额永远 `decimal.Decimal` | 禁止 `float64`/`int64÷100` |
| 8 | 表名 `t_fv_{module}_{name}`、字段 `f_` 前缀 | GORM Model 用 tag `column:` 显式声明 |
| 9 | 7 个抽象接口 | `Repository / CacheProvider / LLMProvider / EventBus / IDGenerator / Migrator / ReportExporter` |
| 10 | 错误码区间 | 10000 通用 / 20000 user / 30000 core / 40000 quote / 50000 ai / 90000 系统 |

---

## 2. 总体架构图

```
┌──────────────────────────────────────────────────────────────────┐
│                     Vue3 SPA（Element Plus）                      │
│   理财录入 ┃ 基金录入 ┃ 股票录入 ┃ 现金 ┃ 持仓 ┃ 流水 ┃ 行情 ┃ AI │
└─────────────────────────────────┬────────────────────────────────┘
                                  │ HTTP / SSE
┌─────────────────────────────────▼────────────────────────────────┐
│              Gin Router（/api/v1/**）+ 中间件链                  │
│   RequestID → Logger(slog) → Recovery → CORS → Auth(JWT)         │
└─────────────────────────────────┬────────────────────────────────┘
                                  │
┌─────────────────────────────────▼────────────────────────────────┐
│                       Handler（参数校验 + 响应）                  │
│  asset │ holding │ transaction │ quote │ rate │ ai │ export      │
└─────────────────────────────────┬────────────────────────────────┘
                                  │
┌─────────────────────────────────▼────────────────────────────────┐
│         Service（业务编排 + 事务 + AI Tool Calling 循环）         │
│  AssetSvc │ HoldingSvc │ TxnSvc │ QuoteSvc │ RateSvc │ ChatSvc   │
│  AdvisorSvc │ AnalysisSvc │ MatureSvc(cron) │ ExportSvc          │
└────┬─────────┬─────────┬─────────┬─────────┬─────────┬───────────┘
     │         │         │         │         │         │
┌────▼───┐ ┌──▼────┐ ┌──▼─────┐ ┌─▼────┐ ┌─▼─────┐ ┌─▼─────────┐
│Repo IF │ │Cache  │ │LLM IF  │ │Event │ │IDGen  │ │ReportExp. │
│        │ │Provider│ │+Tools  │ │Bus   │ │       │ │           │
└────┬───┘ └──┬────┘ └──┬─────┘ └─┬────┘ └─┬─────┘ └─┬─────────┘
     │        │         │         │        │         │
┌────▼────┐┌──▼──────┐┌─▼─────┐┌──▼─────┐┌▼──────┐┌─▼────────┐
│gormrepo ││local /  ││openai ││channel ││uuidv7 ││ excel /   │
│(SQLite/ ││redis    ││Provider││  bus   ││       ││ markdown  │
│Postgres/││         ││+Tool註 ││        ││       ││           │
│MySQL)   ││         ││册中心  ││        ││       ││           │
└─────────┘└─────────┘└────────┘└────────┘└───────┘└───────────┘
```

**升级最小改动的核心保障**：业务层只依赖中间一排「抽象接口」，最下面一排「具体实现」可以替换而上层零改动。

---

## 3. 后端目录结构（最终落地）

```
backend/
├── cmd/
│   └── api/
│       └── main.go                  # 入口：bootstrap.Wire(cfg) → app.Run()
├── internal/
│   ├── bootstrap/
│   │   ├── config.go                # viper 配置加载（只在这里调一次）
│   │   ├── logger.go                # slog 初始化
│   │   ├── db.go                    # DB 工厂（sqlite/postgres/mysql 切换）
│   │   ├── cache.go                 # CacheProvider 工厂
│   │   ├── llm.go                   # LLMRegistry 装配
│   │   ├── cron.go                  # cron 调度装配（理财到期、行情拉取）
│   │   ├── router.go                # Gin 路由注册（中间件按规定顺序）
│   │   └── wire.go                  # 总装函数 Wire(cfg) -> *App
│   ├── domain/                      # 纯结构体 + 枚举（零外部依赖，仅 decimal/time/gorm.DeletedAt）
│   │   ├── base.go                  # BaseModel / 枚举常量
│   │   ├── user.go
│   │   ├── platform.go
│   │   ├── asset.go                 # Asset + FundDetail + StockDetail + WealthDetail
│   │   ├── holding.go               # Holding + HoldingView（计算字段）
│   │   ├── transaction.go
│   │   ├── quote.go                 # PriceQuote + ExchangeRate
│   │   ├── cost_lot.go
│   │   ├── portfolio.go
│   │   ├── ai_chat.go               # AIConversation + AIMessage
│   │   └── report.go
│   ├── repository/                  # Repo 接口（不引入 gorm！）
│   │   ├── interfaces.go            # 所有 Repo 接口 + UnitOfWork + ListOptions
│   │   ├── errors.go                # ErrNotFound / ErrConflict / ErrInsufficientQty
│   │   └── gorm/                    # GORM 实现（唯一允许 import gorm 的地方）
│   │       ├── unitofwork.go        # UnitOfWork 实现，封装事务
│   │       ├── user_repo.go
│   │       ├── platform_repo.go
│   │       ├── asset_repo.go        # 含 fund/stock/wealth detail
│   │       ├── holding_repo.go
│   │       ├── transaction_repo.go
│   │       ├── quote_repo.go
│   │       ├── rate_repo.go
│   │       ├── cost_lot_repo.go
│   │       ├── portfolio_repo.go
│   │       ├── ai_chat_repo.go
│   │       └── report_repo.go
│   ├── service/                     # 业务编排（只依赖接口）
│   │   ├── asset_service.go         # 资产 + 子类型 detail 一体 CRUD
│   │   ├── holding_service.go       # 持仓视图、聚合统计
│   │   ├── transaction_service.go   # 交易写入 + Holding 同步（核心事务）
│   │   ├── quote_service.go         # 行情手动刷新 + 第三方 API 拉取
│   │   ├── rate_service.go          # 汇率维护 + 折算
│   │   ├── mature_service.go        # 理财到期定时（cron@00:30）
│   │   ├── chat_service.go          # AI 流式问答（SSE）
│   │   ├── advisor_service.go       # 买卖/持仓建议（Tool Calling）
│   │   ├── analysis_service.go      # 盈亏分析（Tool Calling）
│   │   ├── import_service.go        # CSV 批量导入
│   │   └── export_service.go        # Excel/Markdown 导出
│   ├── handler/                     # HTTP（Gin）
│   │   ├── asset_handler.go
│   │   ├── holding_handler.go
│   │   ├── transaction_handler.go
│   │   ├── quote_handler.go
│   │   ├── rate_handler.go
│   │   ├── chat_handler.go          # SSE
│   │   ├── advisor_handler.go
│   │   ├── analysis_handler.go
│   │   ├── import_handler.go
│   │   ├── export_handler.go
│   │   └── meta_handler.go          # /healthz /version /platforms 字典
│   ├── middleware/
│   │   ├── request_id.go
│   │   ├── logger.go
│   │   ├── recovery.go
│   │   ├── cors.go
│   │   └── auth.go                  # 第一阶段单用户：Header X-User-Id 默认 1
│   ├── llm/                         # AI 适配层（唯一允许 import go-openai 的地方）
│   │   ├── provider.go              # LLMProvider 接口 + 类型
│   │   ├── openai_provider.go       # 基于 go-openai 的统一实现
│   │   ├── registry.go              # LLMRegistry，按名称路由
│   │   └── tools/                   # Tool 定义（可被 Service 注册到对话）
│   │       ├── registry.go          # ToolRegistry：name -> Tool
│   │       ├── holding_query.go
│   │       ├── market_data.go
│   │       ├── profit_calc.go
│   │       ├── history_query.go
│   │       └── platform_summary.go
│   ├── cache/
│   │   ├── interfaces.go            # CacheProvider
│   │   ├── local.go                 # sync.Map + TTL（默认）
│   │   ├── redis.go                 # go-redis 实现
│   │   └── factory.go
│   ├── event/
│   │   ├── interfaces.go            # EventBus
│   │   └── channel.go               # Go channel 实现
│   ├── id/
│   │   ├── generator.go             # IDGenerator 接口
│   │   └── uuidv7.go                # google/uuid v7 实现
│   ├── report/
│   │   ├── exporter.go              # ReportExporter 接口
│   │   ├── excel.go                 # excelize/v2
│   │   └── markdown.go              # text/template
│   └── platformapi/                 # 第三方行情/汇率适配（resty）
│       ├── eastmoney.go             # 天天基金/东方财富
│       ├── sina.go                  # 新浪股票
│       ├── tencent.go               # 腾讯股票
│       └── pboc.go                  # 央行汇率（可选）
├── pkg/
│   ├── errs/
│   │   ├── code.go                  # 错误码常量（按区间）
│   │   └── error.go                 # AppError + Wrap/Is
│   └── utils/
│       ├── decimalx/                # decimal 辅助（金额折算、四舍五入）
│       ├── timex/                   # 时间辅助（日期边界、cron 时间）
│       └── response/                # 统一响应（code/message/data/request_id）
├── configs/
│   ├── local.yaml                   # SQLite + local cache + 单用户
│   └── prod.yaml.tpl                # PostgreSQL + Redis + JWT 模板
├── migrations/                      # 人工核对的基线 SQL（与 SQL Schema 同源）
│   └── 0001_init.sql
├── go.mod
└── go.sum
```

> **目录规模口径**：第一阶段约 70 个 Go 文件、~6000 行业务代码；任何成员开新文件前先看本表确认归属。

---

## 4. 模块依赖图（关键引用规则）

```
            ┌──────────────────────────┐
            │   handler  (gin)         │
            └──────────────┬───────────┘
                           │ ✅ 允许：service 接口、pkg/errs、pkg/utils
                           ▼
            ┌──────────────────────────┐
            │   service                │
            └──┬───────┬───────┬───────┘
               │       │       │
               │       │       │ ✅ 允许：repository、cache、llm、event、id、report、domain、pkg/*
               │       │       │ ❌ 禁止：gorm、redis、go-openai 直接 import
               ▼       ▼       ▼
       repo IF    cache IF   llm IF (+tools)
               │       │       │
               ▼       ▼       ▼
       gorm impl  local/redis  openai_provider（唯一 go-openai import）
                                   │
                                   ▼
                              go-openai
```

**红线检查**（developer/reviewer 上线前必须扫一遍）：

```bash
# 在 backend 根目录跑
grep -RIn "gorm.io/gorm" internal/service && echo "❌ service 不得 import gorm"
grep -RIn "redis/go-redis" internal/service && echo "❌ service 不得 import redis"
grep -RIn "sashabaranov/go-openai" internal/service && echo "❌ service 不得 import go-openai"
```

---

## 5. 中间件顺序（强制）

```go
r := gin.New()
r.Use(
    middleware.RequestID(),   // 1. 生成/透传 X-Request-Id
    middleware.Logger(),       // 2. slog 结构化日志（含 request_id / latency）
    middleware.Recovery(),     // 3. panic → 90001 系统错误
    middleware.CORS(cfg.CORS), // 4. 跨域（dev 全放开，prod 白名单）
    middleware.Auth(cfg.Auth), // 5. 单用户：取 Header X-User-Id，默认 1；多用户：JWT
)
```

> 顺序不能错：**Recovery 在 Logger 之后**，确保 panic 也能被结构化记录；**Auth 在最后一层**，前面三层中间件即使认证失败也要执行（否则错误日志丢失 request_id）。

---

## 6. 关键抽象接口（最终签名）

> 已与 `docs/architecture-design.md §4` 对齐，本节是 developer 直接 copy 的最小可编译版本。

### 6.1 Repository 接口

```go
// internal/repository/interfaces.go
package repository

import (
    "context"
    "time"
    "github.com/eyjian/fin-vault/backend/internal/domain"
)

type ListOptions struct {
    UserID   uint
    Page     int
    PageSize int
    OrderBy  string // e.g. "f_created_at desc"
    Filters  map[string]any
}

// 事务边界
type UnitOfWork interface {
    Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type AssetRepository interface {
    Create(ctx context.Context, a *domain.Asset) error
    Update(ctx context.Context, a *domain.Asset) error
    GetByID(ctx context.Context, userID, id uint) (*domain.Asset, error)
    GetByCode(ctx context.Context, userID uint, code string, t domain.AssetType) (*domain.Asset, error)
    List(ctx context.Context, opts ListOptions) ([]domain.Asset, int64, error)
    Delete(ctx context.Context, userID, id uint) error // 软删

    UpsertFundDetail(ctx context.Context, d *domain.FundDetail) error
    UpsertStockDetail(ctx context.Context, d *domain.StockDetail) error
    UpsertWealthDetail(ctx context.Context, d *domain.WealthDetail) error
}

type HoldingRepository interface {
    Create(ctx context.Context, h *domain.Holding) error
    Update(ctx context.Context, h *domain.Holding) error
    GetByID(ctx context.Context, userID, id uint) (*domain.Holding, error)
    GetOrCreate(ctx context.Context, userID, assetID, platformID uint) (*domain.Holding, error)
    ListByUser(ctx context.Context, opts ListOptions) ([]domain.Holding, int64, error)
    ListMaturedWealth(ctx context.Context, asOfDate time.Time) ([]domain.Holding, error) // 给 MatureService 用
}

type TransactionRepository interface {
    Create(ctx context.Context, t *domain.Transaction) error
    GetByID(ctx context.Context, userID, id uint) (*domain.Transaction, error)
    List(ctx context.Context, opts ListOptions) ([]domain.Transaction, int64, error)
    ExistsByExternalID(ctx context.Context, userID, platformID uint, externalID string) (bool, error)
}

type QuoteRepository interface {
    Insert(ctx context.Context, q *domain.PriceQuote) error
    GetLatest(ctx context.Context, assetID uint) (*domain.PriceQuote, error)
    BatchGetLatest(ctx context.Context, assetIDs []uint) (map[uint]*domain.PriceQuote, error)
}

type RateRepository interface {
    Insert(ctx context.Context, r *domain.ExchangeRate) error
    GetLatest(ctx context.Context, from, to string, asOf time.Time) (*domain.ExchangeRate, error)
}

type AIConversationRepository interface {
    CreateConv(ctx context.Context, c *domain.AIConversation) error
    AppendMessage(ctx context.Context, m *domain.AIMessage) error
    ListMessages(ctx context.Context, convID uint, limit int) ([]domain.AIMessage, error)
    ListConversations(ctx context.Context, userID uint, opts ListOptions) ([]domain.AIConversation, int64, error)
}

type PlatformRepository interface {
    List(ctx context.Context) ([]domain.Platform, error)
    GetByCode(ctx context.Context, code string) (*domain.Platform, error)
}

// 还有：CostLotRepository / PortfolioRepository / ReportRepository 等，按 domain-model.md 对齐。
```

### 6.2 CacheProvider / EventBus / IDGenerator / Migrator / ReportExporter

→ 直接复用 `architecture-design.md §4.2 / §4.4 / §4.5 / §4.6`，签名不变，本文不再重复。

### 6.3 LLMProvider（核心场景）

```go
// internal/llm/provider.go
package llm

type LLMProvider interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req ChatRequest) (<-chan Chunk, error)
    ChatWithTools(ctx context.Context, req ChatRequest, tools []Tool) (*ChatResponse, error)
}

// 多模型路由
type Registry interface {
    Get(name string) LLMProvider // name 为空走默认
    List() []string
}
```

> 业务侧 Tool Calling 循环写在 `service/advisor_service.go` / `service/analysis_service.go`，最多 30 行模板代码。

---

## 7. 事务边界与 UnitOfWork 用法

```go
// service/transaction_service.go（核心交易写入）
func (s *TxnService) CreateBuy(ctx context.Context, in BuyInput) (*domain.Transaction, error) {
    var out *domain.Transaction
    err := s.uow.Do(ctx, func(ctx context.Context) error {
        // 1. GetOrCreate Holding（带写锁）
        h, err := s.holdingRepo.GetOrCreate(ctx, in.UserID, in.AssetID, in.PlatformID)
        if err != nil { return err }

        // 2. 校验 + 计算
        if err := validateBuy(in); err != nil { return err }
        netAmount := in.Amount.Add(in.Fee).Add(in.Tax)
        newQty   := h.Quantity.Add(in.Quantity)
        newCost  := h.TotalCost.Add(netAmount)
        avgCost  := newCost.Div(newQty)

        // 3. 写 Transaction
        t := &domain.Transaction{ /* ...buy 字段 */ }
        if err := s.txnRepo.Create(ctx, t); err != nil { return err }

        // 4. 更新 Holding
        h.Quantity, h.TotalCost, h.AvgCost = newQty, newCost, avgCost
        h.LastTxnAt = &in.TxnTime
        if h.FirstBuyAt == nil { h.FirstBuyAt = &in.TxnTime }
        if err := s.holdingRepo.Update(ctx, h); err != nil { return err }

        // 5. (可选) 写 CostLot（cost_method=fifo 时）
        if h.CostMethod == domain.CostMethodFIFO { /* ... */ }

        out = t
        return nil
    })
    return out, err
}
```

**强制事务的 4 类操作**（与 `database-schema.md §6.3` 对齐）：

1. 创建/更新交易 → 必同步更新 Holding（FIFO 时还含 CostLot）
2. 现金联动 → 2 条 Transaction + 2 个 Holding 更新
3. 理财到期 → Transaction (mature) + Holding(matured) + WealthDetail(状态)
4. 重算持仓 → Holding 重写 + CostLot 重建（按 Transaction 回放）

---

## 8. AI 编排（Tool Calling 循环模板）

```go
// service/advisor_service.go
func (s *AdvisorSvc) Recommend(ctx context.Context, userID uint, prompt string) (string, error) {
    provider := s.llm.Get("") // 默认 provider
    tools := s.toolReg.PickAll() // 5 个 Tool
    msgs := []llm.Message{
        {Role: "system", Content: advisorSystemPrompt},
        {Role: "user", Content: prompt},
    }
    for i := 0; i < maxToolHops; i++ { // maxToolHops=5，防死循环
        resp, err := provider.ChatWithTools(ctx, llm.ChatRequest{Messages: msgs}, tools)
        if err != nil { return "", err }
        msgs = append(msgs, llm.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
        if resp.FinishReason != "tool_calls" {
            return resp.Content, nil
        }
        for _, tc := range resp.ToolCalls {
            tool, _ := s.toolReg.Get(tc.Name)
            result, _ := tool.Handler(ctx, tc.Arguments) // 错误以字符串返回，不打断对话
            msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: result})
        }
    }
    return "", errs.New(50002, "tool calling exceeded max hops")
}
```

**ChatService 流式（SSE）要点**：

- Header: `Content-Type: text/event-stream` + `Cache-Control: no-cache` + `Connection: keep-alive`
- 用 `c.Stream(func(w io.Writer) bool { ... })` 持续推送 `Chunk{Content, Done}`
- 每条 chunk 一行 `data: {json}\n\n`
- 客户端断开 → `c.Request.Context().Done()` 触发清理

---

## 9. Cron 调度

| 任务 | 表达式 | 实现 | 入口 |
|---|---|---|---|
| 理财到期扫描 | `30 0 * * *`（每天 00:30） | `MatureService.RunOnce` | `bootstrap/cron.go` |
| 行情批量刷新（可选） | `*/15 9-15 * * 1-5`（A 股交易时段每 15 分钟） | `QuoteService.RefreshAll` | 同上 |
| 缓存预热（可选） | `0 0 * * *` | `HoldingService.WarmupSummary` | 同上 |

启动入口：

```go
// bootstrap/cron.go
c := cron.New(cron.WithSeconds(false), cron.WithLogger(slogAdapter))
c.AddFunc("30 0 * * *", func() { _ = matureSvc.RunOnce(ctx) })
c.Start()
```

---

## 10. 配置项（与 architecture-design.md §6 对齐）

第一阶段 `configs/local.yaml`：

```yaml
server:
  port: 8080
  mode: debug
  cors_origins: ["*"]
database:
  driver: sqlite
  dsn: "./data/fin-vault.db"
  auto_migrate: true
  log_level: warn
cache:
  driver: local
llm:
  default: deepseek
  providers:
    deepseek:
      api_key: "${DEEPSEEK_API_KEY}"
      base_url: "https://api.deepseek.com"
      model: "deepseek-chat"
    glm:
      api_key: "${GLM_API_KEY}"
      base_url: "https://open.bigmodel.cn/api/paas/v4"
      model: "glm-4-air"
    kimi:
      api_key: "${KIMI_API_KEY}"
      base_url: "https://api.moonshot.cn/v1"
      model: "moonshot-v1-8k"
    qwen:
      api_key: "${QWEN_API_KEY}"
      base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
      model: "qwen-plus"
    ollama:
      api_key: "ollama"
      base_url: "http://127.0.0.1:11434/v1"
      model: "qwen2.5:7b"
log:
  level: debug
  format: console
auth:
  mode: local        # local / jwt
  default_user_id: 1
security:
  encryption_key: "${ENCRYPTION_KEY:dev-only-do-not-use-in-prod}"
quote:
  source_priority: ["eastmoney", "sina", "tencent"]
cron:
  mature: "30 0 * * *"
```

---

## 11. 前端架构（Vue3）

> 用户特别强调：**「理财产品、基金、股票」录入和管理页面是必交付项**，不能漏。

### 11.1 选型与目录

- 框架：Vue 3 + Vite
- 状态：Pinia
- 路由：Vue Router 4
- UI：Element Plus（PC 后台首选，组件齐、文档好）
- 请求：axios（统一拦截器：`X-Request-Id`、错误码）
- 图表：ECharts（持仓饼图、盈亏曲线）

```
frontend/
├── src/
│   ├── api/              # axios 接口封装（按后端 data-interfaces.yaml 一对一）
│   │   ├── asset.ts
│   │   ├── holding.ts
│   │   ├── transaction.ts
│   │   ├── quote.ts
│   │   ├── rate.ts
│   │   ├── ai.ts
│   │   └── export.ts
│   ├── stores/           # Pinia
│   │   ├── platform.ts   # 平台字典（启动加载一次）
│   │   ├── ui.ts         # 全局 UI 状态
│   │   └── chat.ts       # AI 对话会话状态（含 SSE 状态）
│   ├── views/
│   │   ├── dashboard/    # 总览
│   │   ├── wealth/       # ★ 理财产品录入与管理
│   │   ├── fund/         # ★ 基金录入与管理
│   │   ├── stock/        # ★ 股票录入与管理
│   │   ├── cash/         # 现金账户
│   │   ├── holding/      # 持仓视图
│   │   ├── transaction/  # 交易流水
│   │   ├── quote/        # 行情管理
│   │   ├── rate/         # 汇率维护
│   │   ├── ai-chat/      # AI 对话（SSE 流式）
│   │   └── export/       # 数据导出
│   ├── components/
│   │   ├── AssetForm.vue # 资产录入通用组件，按 type 切换 detail
│   │   ├── TxnDialog.vue # 交易录入弹窗
│   │   ├── MoneyInput.vue# 金额输入（绑定 decimal 字符串，不丢精度）
│   │   └── SSEChat.vue   # SSE 接收组件
│   ├── router/index.ts
│   ├── App.vue
│   └── main.ts
├── index.html
├── package.json
├── tsconfig.json
└── vite.config.ts
```

### 11.2 必交付页面清单（M1 验收口径）

| 页面 | 关键能力 |
|---|---|
| `/wealth` 理财产品 | 录入（产品类型、起息/到期日、预期年化、起购金额）+ 列表 + 编辑 + 持仓查看 + 到期提醒标记 |
| `/fund` 基金 | 录入（代码、类型、经理、公司、净值）+ 列表 + 净值刷新按钮 + 持仓 + 申购/赎回流水录入 |
| `/stock` 股票 | 录入（代码、市场 SH/SZ/HK/US/BJ、行业）+ 列表 + 行情刷新 + 持仓 + 买/卖/分红/拆股流水 |
| `/cash` 现金 | 平台×币种现金账户、充提/利息流水 |
| `/holding` 持仓 | 总览（按平台/类型/币种聚合）+ 切换原币种 / CNY 折算 |
| `/transaction` 流水 | 多条件筛选 + CSV 导入 + 编辑 + 删除 |
| `/ai-chat` AI 对话 | 4 场景切换 + 模型切换（DeepSeek/GLM/Kimi/通义/Ollama）+ SSE 流式 |
| `/export` 导出 | Excel / Markdown 一键导出 |

---

## 12. 与三件套的差异说明（对齐声明）

| 项 | 三件套 | 本文 | 差异原因 |
|---|---|---|---|
| 入口路径 | `cmd/server/main.go` | `cmd/api/main.go` | 与 workspace skill `go-gin-development` 的「五层目录铁律」对齐 |
| 配置目录 | `backend/configs/` | 同 | 一致 |
| Repo 实现包 | `repository/gorm/` | 同 | 一致 |
| AI 适配 | `internal/llm/` | 同 + `internal/llm/tools/`（拆 ToolRegistry） | 仅细化，不冲突 |
| 错误处理 | 散落 | 统一 `pkg/errs` + 错误码区间 | 实施落地补强 |
| 中间件 | 未明确顺序 | 明确 5 层顺序 | 实施落地补强 |
| 第三方 API | 未独立目录 | 拆 `internal/platformapi/` | 隔离 resty 依赖 |

> 原则：**三件套定方向 + 本文定细节**，发生冲突时以本文为施工依据，并由架构师同步回写三件套。

---

## 13. 一阶段不做（与 REQUIREMENT §3 对齐）

- ❌ 报表生成（建表 + Repo 占位即可，Service/Handler 推迟）
- ❌ Wire / golang-migrate / snowflake / gopdf / Eino
- ❌ 多用户体系（user_id 字段已预留，单用户固定 ID=1）

---

## 14. 验收清单（DoD）

后端 M1：

- [ ] `cmd/api/main.go` 启动 8080，`GET /healthz` 返回 200
- [ ] 13 张表全部 AutoMigrate 通过，初始化数据写入（user/平台/汇率）
- [ ] 资产 CRUD（基金/股票/理财/现金）+ 子类型 detail 一体化
- [ ] 持仓视图 + 实时计算（market_value / pnl）
- [ ] 交易 13 种类型全部覆盖 + 事务边界验证（buy/sell/mature 端到端）
- [ ] 行情手动刷新 + 东方财富 / 新浪 任一接通
- [ ] 多币种折算（HKD/USD → CNY）
- [ ] 理财到期定时任务（00:30 跑通）
- [ ] AI 对话（DeepSeek + 至少一个国产模型）+ SSE 流式
- [ ] 导出 Excel + Markdown
- [ ] 红线检查脚本通过（service 不引 gorm/redis/openai）

前端 M1：

- [ ] 8 个核心页面（含理财/基金/股票录入管理）能跑通
- [ ] 与后端 `data-interfaces.yaml` 一对一对齐
- [ ] AI 对话 SSE 正常流式

---

## 15. 演进锚点（升级最小改动）

| 触发条件 | 行动 |
|---|---|
| 接 PostgreSQL/MySQL | 改 `bootstrap/db.go` + 引 `golang-migrate` |
| 多实例部署 | `cache.driver=redis` + `event` 加 `nats.go` |
| 复杂 Multi-Agent | 新增 `llm/eino_provider.go` 实现 `LLMProvider` |
| 微信小程序 | 新增 `handler/mini/*`，复用 service |
| 多用户 SaaS | 启用 JWT、Repo 层补 user_id 校验、配额表 |

> 详见 `docs/upgrade-guide.md`。
