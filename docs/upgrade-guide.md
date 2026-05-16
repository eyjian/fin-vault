# FinVault（锦仓）升级改造指南

> 文档版本：v1.0  
> 创建日期：2026-05-16  
> 状态：已确认

本文档是 FinVault 从本地单机部署升级到分布式/商业化部署的操作手册。每个升级场景都给出了具体的改动点、需要新增的文件和不需要动的文件。

核心原则：**业务代码（service/、agent/、handler/、domain/）永远不需要改，只改基础设施层的工厂函数、配置文件，以及新增实现文件。**

---

## 1. 升级总览

| 维度 | 本地阶段 | 分布式升级 | 代码改动量 |
|------|---------|-----------|-----------|
| 数据库 | SQLite | PostgreSQL / MySQL / TDSQL | 改配置文件 |
| 缓存 | 进程内 map | Redis 单机/集群 | 改配置文件 |
| AI 模型 | langchaingo 直连 | 不变（可加限流中间件） | 无 |
| 事件总线 | Go channel | NATS / Kafka | 新增实现文件 + 改工厂注册 |
| 会话管理 | 内存 map | Redis Session | 新增实现文件 + 改配置 |
| 定时任务 | Go cron 单机 | etcd 选主分布式调度 | 新增实现文件 + 改配置 |
| 认证鉴权 | 简单密码 | JWT / OAuth2 | 新增中间件 |
| 用户体系 | 单用户 | 多用户 + 数据隔离 | 新增模块 |
| 日志 | 本地文件/控制台 | 结构化 JSON + ELK | 改配置 |
| 微信小程序 | 无 | 新增小程序认证+API | 新增中间件+Handler |

---

## 2. 数据库升级：SQLite → PostgreSQL / MySQL / TDSQL

### 2.1 改动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 修改 | `configs/prod.yaml` | `database.driver` 改为 `postgres` 或 `mysql`，`database.dsn` 改为对应连接串 |
| 确认 | `internal/database/postgres.go` | 已有初始化函数 |
| 确认 | `internal/database/mysql.go` | 已有初始化函数 |
| 执行 | 数据迁移 | 运行 GORM AutoMigrate 或手动迁移脚本 |
| 无需改动 | `internal/repository/gorm/*.go` | GORM 实现与驱动无关 |
| 无需改动 | `internal/service/*.go` | 只依赖 Repository 接口 |
| 无需改动 | `internal/handler/*.go` | 只依赖 Service |

### 2.2 操作步骤

1. 安装目标数据库（PostgreSQL / MySQL / TDSQL）
2. 创建数据库和用户
3. 修改配置文件：
   ```yaml
   database:
     driver: postgres    # 或 mysql / tdsql
     dsn: "host=db port=5432 user=finvault password=xxx dbname=finvault sslmode=disable"
   ```
4. 启动服务，GORM AutoMigrate 自动建表
5. 如需迁移已有 SQLite 数据，编写一次性数据迁移脚本

### 2.3 迁移脚本参考

```go
// scripts/migrate_sqlite_to_pg.go
// 从 SQLite 读取数据，写入 PostgreSQL
// 利用相同的 GORM Model，只是连接不同的数据库实例
func migrateData(sqlitePath string, pgDSN string) {
    srcDB, _ := gorm.Open(sqlite.Open(sqlitePath), &gorm.Config{})
    dstDB, _ := gorm.Open(postgres.Open(pgDSN), &gorm.Config{})

    // 迁移 Holdings
    var holdings []Holding
    srcDB.Find(&holdings)
    dstDB.Create(&holdings)

    // 迁移 Transactions
    var transactions []Transaction
    srcDB.Find(&transactions)
    dstDB.Create(&transactions)
    // ... 其他表
}
```

### 2.4 注意事项

- SQLite 的某些数据类型在 PostgreSQL 中需要调整（如 BOOLEAN、DATETIME）
- SQLite 自增 ID 从 1 开始，PostgreSQL 用序列，迁移时注意 ID 映射
- TDSQL 兼容 MySQL 协议，使用 `mysql` 驱动即可

---

## 3. 缓存升级：进程内缓存 → Redis

### 3.1 改动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 修改 | `configs/prod.yaml` | `cache.driver` 改为 `redis`，填写 Redis 连接信息 |
| 已有 | `internal/cache/redis.go` | Redis 缓存实现 |
| 已有 | `internal/cache/factory.go` | 工厂函数已支持 redis 分支 |
| 无需改动 | `internal/service/*.go` | 只依赖 CacheProvider 接口 |
| 无需改动 | `internal/handler/*.go` | 不直接使用缓存 |

### 3.2 操作步骤

1. 部署 Redis（单机或集群）
2. 修改配置文件：
   ```yaml
   cache:
     driver: redis
     redis:
       addr: "redis:6379"
       password: "xxx"
       db: 0
   ```
3. 启动服务，工厂函数自动选择 Redis 实现

### 3.3 Redis 集群配置

```yaml
cache:
  driver: redis
  redis:
    mode: cluster    # 单机模式省略此字段
    addrs:
      - "redis1:6379"
      - "redis2:6379"
      - "redis3:6379"
    password: "xxx"
```

需在 `internal/cache/redis.go` 中增加集群模式初始化逻辑。

---

## 4. AI 模型切换与扩展

### 4.1 切换已有模型

只需修改配置文件：

```yaml
llm:
  default: glm    # 将默认模型从 deepseek 切换为 glm
```

### 4.2 新增模型

三个步骤：

**步骤 1**：在配置文件中添加新模型配置

```yaml
llm:
  providers:
    # 已有模型...
    qwen:                           # 新增通义千问
      api_key: "${QWEN_API_KEY}"
      base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
      model: "qwen-plus"
```

**步骤 2**：如果新模型兼容 OpenAI 协议，无需写代码；如不兼容，新增 Provider 实现

```go
// agent/qwen.go（仅在不兼容 OpenAI 协议时需要）

type qwenProvider struct {
    // 自定义实现
}

func (p *qwenProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
    // 自定义调用逻辑
}
// ... 实现 LLMProvider 接口的其他方法
```

**步骤 3**：在 `LLMRegistry` 注册新 Provider

```go
// agent/registry.go 中 NewLLMRegistry 函数增加分支
case "qwen":
    registry.providers["qwen"] = newQwenProvider(cfg)
```

### 4.3 AI 限流（商业化场景）

商业化多用户场景下，需要对 AI 调用加限流：

```go
// middleware/llm_ratelimit.go（新增文件）

func LLMRateLimit(cache cache.CacheProvider) gin.HandlerFunc {
    return func(c *gin.Context) {
        userID := c.GetString("user_id")
        key := fmt.Sprintf("llm_ratelimit:%s:%s", userID, time.Now().Format("2006-01-02"))
        // 基于 Redis 的滑动窗口限流
        // ...
    }
}
```

在路由注册时加中间件即可，不影响业务代码。

---

## 5. 事件总线升级：Go channel → NATS / Kafka

### 5.1 改动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 新增 | `internal/event/nats.go` | NATS EventBus 实现 |
| 修改 | `internal/event/factory.go` | 工厂函数增加 NATS 分支 |
| 修改 | `configs/prod.yaml` | 增加 event 配置 |
| 无需改动 | `internal/service/*.go` | 只依赖 EventBus 接口 |
| 无需改动 | `internal/agent/*.go` | 只依赖 EventBus 接口 |

### 5.2 NATS 实现参考

```go
// internal/event/nats.go

type natsBus struct {
    conn       *nats.Conn
    handlers   map[string][]EventHandler
    mu         sync.RWMutex
}

func newNATSBus(cfg *config.EventConfig) *natsBus {
    conn, _ := nats.Connect(cfg.NATS.URL)
    return &natsBus{conn: conn, handlers: make(map[string][]EventHandler)}
}

func (b *natsBus) Publish(ctx context.Context, event Event) error {
    data, _ := json.Marshal(event.Payload)
    return b.conn.Publish(event.Topic, data)
}

func (b *natsBus) Subscribe(topic string, handler EventHandler) error {
    _, err := b.conn.Subscribe(topic, func(msg *nats.Msg) {
        var payload any
        json.Unmarshal(msg.Data, &payload)
        handler(context.Background(), Event{Topic: topic, Payload: payload})
    })
    return err
}
```

### 5.3 配置变更

```yaml
# 本地阶段（默认，不需要显式配置）
event:
  driver: channel

# 分布式阶段
event:
  driver: nats
  nats:
    url: "nats://nats:4222"
```

---

## 6. 多用户与 SaaS 升级

### 6.1 新增模块

| 模块 | 文件 | 说明 |
|------|------|------|
| 用户模型 | `internal/domain/user.go` | User 结构体 |
| 用户仓储 | `internal/repository/gorm/user_repo.go` | 用户 CRUD |
| 认证服务 | `internal/service/auth_service.go` | 注册、登录、Token 管理 |
| JWT 中间件 | `internal/middleware/jwt.go` | Token 校验 |
| 用户 Handler | `internal/handler/auth_handler.go` | 登录注册 API |

### 6.2 数据隔离方案

在 Repository 层增加 `user_id` 过滤条件：

```go
// 本地阶段
func (r *gormHoldingRepo) List(ctx context.Context, opts ListOptions) ([]Holding, int64, error) {
    // 直接查询，无 user_id 过滤
}

// 多用户阶段
func (r *gormHoldingRepo) List(ctx context.Context, opts ListOptions) ([]Holding, int64, error) {
    query := r.db.WithContext(ctx)
    if opts.UserID != 0 {
        query = query.Where("user_id = ?", opts.UserID)  // 新增过滤条件
    }
    // ... 其余逻辑不变
}
```

### 6.3 数据库表变更

所有业务表增加 `user_id` 字段：

```go
// 迁移脚本
ALTER TABLE holdings ADD COLUMN user_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE transactions ADD COLUMN user_id BIGINT NOT NULL DEFAULT 1;
-- ... 其他表
CREATE INDEX idx_holdings_user_id ON holdings(user_id);
```

本地单用户数据默认 user_id = 1。

---

## 7. 微信小程序接入

### 7.1 新增内容

| 内容 | 文件 | 说明 |
|------|------|------|
| 微信登录中间件 | `internal/middleware/wechat.go` | wx.login code 换 openid |
| 小程序 API 路由 | `internal/handler/routes.go` 中新增分组 | `/api/v1/mini/` 路由组 |
| 小程序前端 | `frontend/mini/` 或独立仓库 | 微信小程序代码 |

### 7.2 API 版本规划

```
/api/v1/          # Web 端 API（现有）
/api/v1/mini/     # 小程序专用 API（新增，增加微信认证）
/api/v1/admin/    # 管理后台 API（商业化后新增）
```

### 7.3 微信小程序升级改动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 新增 | `internal/middleware/wechat.go` | 微信登录认证中间件 |
| 新增 | `internal/handler/wechat_auth_handler.go` | 微信 code2session 登录 |
| 新增 | `internal/domain/wechat_user.go` | 微信用户绑定模型 |
| 新增 | `internal/repository/gorm/wechat_user_repo.go` | 微信用户仓储 |
| 修改 | `internal/handler/routes.go` | 新增 `/api/v1/mini/` 路由组 |
| 修改 | `internal/middleware/auth.go` | 支持 Web Session 和微信 Token 双认证 |
| 修改 | `configs/prod.yaml` | 新增微信小程序 appid/secret 配置 |
| 无需改动 | `internal/service/*.go` | 业务逻辑不变 |
| 无需改动 | `internal/agent/*.go` | Agent 逻辑不变 |
| 无需改动 | `internal/repository/gorm/*_repo.go` | 数据访问不变 |

### 7.4 微信登录流程

```
小程序 wx.login() → code → 后端 /api/v1/mini/auth/login
                                    ↓
                        调用微信 code2session API 获取 openid
                                    ↓
                        查找或创建 WechatUser 绑定记录
                                    ↓
                        生成 JWT Token 返回小程序
```

### 7.5 微信小程序配置

```yaml
# configs/prod.yaml
wechat:
  mini:
    appid: "${WECHAT_MINI_APPID}"
    secret: "${WECHAT_MINI_SECRET}"
```

---

## 8. 定时任务升级：Go ticker → 分布式调度

### 8.1 改动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 新增 | `internal/scheduler/scheduler.go` | 分布式调度器接口定义 |
| 新增 | `internal/scheduler/etcd_scheduler.go` | 基于 etcd 的分布式锁调度（推荐） |
| 修改 | `internal/scheduler/factory.go` | 工厂函数增加分布式调度分支 |
| 修改 | `configs/prod.yaml` | 增加 scheduler 配置 |
| 无需改动 | `internal/service/*.go` | Service 中的业务逻辑不变 |
| 无需改动 | `internal/agent/*.go` | Agent 逻辑不变 |

### 8.2 调度器抽象接口

```go
// internal/scheduler/interfaces.go

type Job struct {
    Name     string
    CronExpr string          // cron 表达式
    Handler  func(ctx context.Context) error
}

type Scheduler interface {
    Register(job Job) error
    Start() error
    Stop() error
}
```

本地阶段实现——基于 `robfig/cron` 的单机调度：

```go
// internal/scheduler/local_scheduler.go

type localScheduler struct {
    cron *cron.Cron
}

func newLocalScheduler() *localScheduler {
    return &localScheduler{cron: cron.New()}
}

func (s *localScheduler) Register(job Job) error {
    return s.cron.AddFunc(job.CronExpr, func() {
        job.Handler(context.Background())
    })
}
```

分布式阶段实现——基于 etcd 选主 + robfig/cron：

```go
// internal/scheduler/etcd_scheduler.go

type etcdScheduler struct {
    cron    *cron.Cron
    client  *clientv3.Client
    lease   *lease.Client
    isLeader bool
}

// 同一时刻只有一个实例获得锁并执行定时任务
// 其他实例作为热备，leader 故障时自动接管
```

### 8.3 配置变更

```yaml
# 本地阶段（默认）
scheduler:
  driver: local

# 分布式阶段
scheduler:
  driver: etcd
  etcd:
    endpoints:
      - "etcd1:2379"
      - "etcd2:2379"
      - "etcd3:2379"
    lock_prefix: "/finvault/scheduler/"
```

### 8.4 常用定时任务

| 任务 | Cron 表达式 | 说明 |
|------|-----------|------|
| 行情数据同步 | `0 9,15 * * 1-5` | 工作日 9:00 和 15:00 同步 |
| 净值数据同步 | `0 20 * * 1-5` | 工作日 20:00 同步基金净值 |
| 到期提醒检查 | `0 8 * * *` | 每天 8:00 检查理财到期 |
| 日收益计算 | `0 21 * * *` | 每天 21:00 计算当日收益 |
| 周报生成 | `0 10 * * 6` | 每周六 10:00 生成周报 |
| 月报生成 | `0 10 1 * *` | 每月 1 日 10:00 生成月报 |

---

## 9. 日志升级：本地文件 → 结构化日志 + ELK

### 9.1 改动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 修改 | `internal/config/config.go` | 日志配置增加 ELK 相关字段 |
| 修改 | `internal/middleware/logger.go` | 日志中间件适配结构化输出 |
| 无需改动 | `internal/service/*.go` | 业务逻辑不直接依赖日志格式 |
| 无需改动 | `internal/handler/*.go` | Handler 不关心日志输出方式 |

### 9.2 日志库选型

本地阶段使用 Go 标准库 `log` 或 `slog`（Go 1.21+）；商业化阶段可切换为 `zap` + ELK：

```go
// 日志抽象，方便后续切换
type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
}

type Field struct {
    Key   string
    Value any
}
```

### 9.3 配置变更

```yaml
# 本地阶段
log:
  level: debug
  format: console    # 控制台友好输出

# 生产阶段
log:
  level: info
  format: json       # 结构化 JSON，方便 ELK 采集
  output: /var/log/finvault/app.log
  max_size: 200      # MB
  max_backups: 10
  max_age: 30        # 天
```

---

## 10. 分布式部署架构参考

```
                    ┌─────────────┐
                    │   Nginx     │
                    │  (负载均衡)  │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
        ┌─────┴─────┐┌────┴─────┐┌────┴─────┐
        │ FinVault  ││ FinVault ││ FinVault │
        │ 实例 1    ││ 实例 2   ││ 实例 3   │
        └─────┬─────┘└────┬─────┘└────┬─────┘
              │            │            │
        ┌─────┴────────────┴────────────┴─────┐
        │           Redis 集群                  │
        │     (Session / Cache / RateLimit)    │
        └─────┬────────────────────────────────┘
              │
        ┌─────┴────────────┐
        │   PostgreSQL     │
        │   (主从/集群)     │
        └──────────────────┘
```

关键依赖组件：
- **Nginx**：负载均衡、SSL 终止
- **Redis**：Session 共享、缓存、限流计数
- **PostgreSQL**：主数据库（推荐主从复制）
- **NATS**（可选）：跨实例事件广播

---

## 11. 升级检查清单

每次升级时，按以下清单逐项确认：

- [ ] 配置文件已修改（`configs/prod.yaml`）
- [ ] 新增的基础设施实现文件已编写
- [ ] 工厂函数已注册新实现
- [ ] 数据库迁移脚本已准备
- [ ] 环境变量已设置（API Key、数据库密码等）
- [ ] 业务代码（service/、agent/、handler/、domain/）未修改
- [ ] 集成测试通过
- [ ] 回滚方案已准备
- [ ] 定时任务调度方式已确认（local / etcd）
- [ ] 日志格式已切换（console / json）
- [ ] 微信小程序配置已准备（如涉及）
- [ ] 新增中间件已注册到路由（JWT / 微信认证 / 限流等）
