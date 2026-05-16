# Go + Gin 开发规范（fin-vault 项目级）

> 本 Skill **覆盖** ai-rd-team 内置的 `go-kratos-development`。
> 本项目用 **Gin** 而非 Kratos，禁止引入 Kratos / GoFrame / Hertz。
> 通用 Go 语法不重复，本文聚焦 fin-vault 工程层落地模式。

## 适用场景

- developer 写后端 HTTP 接口、Service、Repository、定时任务时
- architect 设计新模块的目录结构、包依赖时
- reviewer 审 Gin handler、GORM 用法、Viper 配置加载时
- tester 为 Service / Repository 写单元测试时

不适用于：前端 Vue 代码、纯算法实现、Shell 脚本。

## 核心原则

### 1. 五层目录铁律（CLEAN ARCH 简化版）

```
cmd/api/main.go              ← 入口，仅做 wire 组装
internal/
  domain/                    ← 实体 + 枚举常量，零业务依赖
  repository/
    interfaces.go            ← 仓储接口（context + domain only）
    gorm/                    ← GORM 实现
    cache/                   ← 缓存装饰器
  service/                   ← 业务逻辑（依赖接口）
  handler/                   ← Gin handler
  llm/                       ← LLM Provider 抽象与实现
  bootstrap/                 ← Wire 组装、配置加载、迁移
pkg/
  errs/                      ← 业务错误码
  utils/                     ← 通用工具（response、time、decimal）
```

依赖方向**单向向内**：handler → service → repository（接口）→ gorm 实现。
反向依赖一律拒绝（reviewer 见到直接打回，无须讨论）。

### 2. 一切外部副作用都走接口

`*gorm.DB` / `*redis.Client` / `*openai.Client` **不得**出现在 service 包的导入里。
service 只见到 `Repository` / `CacheProvider` / `LLMProvider` 等接口。

### 3. 配置只读取一次

`viper` 只在 `bootstrap.LoadConfig()` 里读，结果填入强类型 `Config` 结构体，向下游传 `Config` 而非 `*viper.Viper`。

### 4. 路由按业务分组，中间件链最少化

```go
v1 := r.Group("/api/v1")
v1.Use(middleware.RequestID(), middleware.Logger(), middleware.Recovery())

assets := v1.Group("/assets")
assets.Use(middleware.Auth())          // 仅鉴权敏感分组挂 Auth
assets.GET("", h.ListAssets)
assets.POST("", h.CreateAsset)
```

中间件顺序固定：**RequestID → Logger → Recovery → Auth → 业务**。
新增中间件需在 PR 描述中说明插入位置和原因。

### 5. GORM 事务边界写在 Service 层

跨表写操作（如 buy Transaction + Holding 重算）必须在**单个事务**内完成。事务函数签名固定：

```go
func (s *txnService) Buy(ctx context.Context, in BuyInput) (*Transaction, error) {
    var out *Transaction
    err := s.uow.WithTx(ctx, func(tx repository.Tx) error {
        // 用 tx 包装的 repo 操作
        ...
        return nil
    })
    return out, err
}
```

`UnitOfWork` 接口由 repository 层提供，service 不感知 `*gorm.DB`。

## 常用模式

### 模式 A：Handler 三段式

```go
func (h *AssetHandler) Create(c *gin.Context) {
    var req dto.CreateAssetReq
    if err := c.ShouldBindJSON(&req); err != nil {
        response.BadRequest(c, errs.InvalidParam.WithDetail(err.Error()))
        return
    }
    asset, err := h.svc.Create(c.Request.Context(), req.ToInput())
    if err != nil {
        response.Error(c, err)
        return
    }
    response.OK(c, dto.FromAsset(asset))
}
```

三段：**绑参 + 校验 → 调 Service → 统一响应**。Handler 不写业务判断、不直接操作 DB / Cache / LLM。

### 模式 B：Repository 接口 + GORM 实现分离

```go
// internal/repository/interfaces.go
type AssetRepository interface {
    Create(ctx context.Context, a *domain.Asset) error
    GetByID(ctx context.Context, id uint64) (*domain.Asset, error)
    ListByUser(ctx context.Context, userID uint64, opt ListOption) ([]*domain.Asset, int64, error)
}

// internal/repository/gorm/asset_repo.go
type assetRepo struct{ db *gorm.DB }

func NewAssetRepo(db *gorm.DB) repository.AssetRepository { return &assetRepo{db: db} }

func (r *assetRepo) GetByID(ctx context.Context, id uint64) (*domain.Asset, error) {
    var a domain.Asset
    if err := r.db.WithContext(ctx).First(&a, id).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return nil, errs.NotFound
        }
        return nil, fmt.Errorf("asset_repo.GetByID: %w", err)
    }
    return &a, nil
}
```

### 模式 C：Viper 多源配置

加载顺序（后者覆盖前者）：
1. `configs/config.yaml`（默认）
2. `configs/config.{env}.yaml`（环境差异）
3. 环境变量（`FV_DB_DSN` 等，前缀 `FV_`，下划线对应嵌套）

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

### 模式 D：context 全链路传递

```go
// ❌ 不要
func (s *svc) Foo() { db.Find(&x) }
// ✅ 要
func (s *svc) Foo(ctx context.Context) { db.WithContext(ctx).Find(&x) }
```

每个对外方法第一个参数必须是 `ctx context.Context`。GORM 调用必须 `WithContext(ctx)`，否则 reviewer 直接打回。

### 模式 E：定时任务用 robfig/cron

```go
c := cron.New(cron.WithSeconds())
_, _ = c.AddFunc("0 0 2 * * *", func() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    if err := wealthSvc.ScanMatured(ctx); err != nil {
        slog.Error("scan_matured_failed", "err", err)
    }
})
c.Start()
```

定时任务函数自己拿 `context.Background()`，**不要**复用请求 context。

## 禁止

| ❌ 反模式 | ✅ 正确做法 |
|---|---|
| service 包 `import "gorm.io/gorm"` | 只 import 自家 `repository` 接口包 |
| handler 直接调 `db.Find` | handler → service → repository |
| 全局 `var DB *gorm.DB` | 通过 Wire 组装注入 |
| 路由散落各 handler 的 `init()` | 在 `bootstrap.RegisterRoutes()` 集中注册 |
| `*viper.Viper` 在业务函数里 | 业务只见 `Config` 强类型 |
| 多个 Recovery / Logger 中间件嵌套 | 全局只挂一次 |
| 用 `panic` 表达业务错误 | 返回 `error`，仅 unrecoverable 才 panic |
| `time.Now()` 散落业务代码 | 通过 `Clock` 接口注入（便于测试） |
| 在 handler 里写 SQL 字符串 | SQL 只在 `repository/gorm/` 里出现 |
| 引入 Hertz / GoFrame / Kratos | 项目锁死 Gin |

## 示例

### 完整模块骨架（asset 模块）

```
internal/
├── domain/
│   └── asset.go                  # type Asset struct{...}
├── repository/
│   ├── interfaces.go             # AssetRepository interface
│   └── gorm/
│       └── asset_repo.go         # struct assetRepo{db *gorm.DB}
├── service/
│   ├── asset.go                  # type AssetService interface + impl
│   └── asset_test.go             # 用 mock repo 测试
├── handler/
│   ├── asset.go                  # AssetHandler.Create/Get/List
│   └── asset_dto.go              # CreateAssetReq / AssetResp
└── bootstrap/
    ├── wire.go                   # ProvideAssetService(repo) AssetService
    └── routes.go                 # v1.POST("/assets", h.Create)
```

### Service 测试（用 mock 仓储，零 DB 依赖）

```go
func TestAssetService_Create_DuplicateCode(t *testing.T) {
    mockRepo := mocks.NewAssetRepository(t)
    mockRepo.On("ExistsByCode", mock.Anything, "F000001").Return(true, nil)

    svc := service.NewAssetService(mockRepo, nil, nil)
    _, err := svc.Create(context.Background(), service.CreateInput{
        Code: "F000001", Name: "测试基金",
    })
    require.ErrorIs(t, err, errs.AssetCodeDuplicated)
}
```

### Repository 测试（用 go-sqlmock，零真实 DB）

```go
func TestAssetRepo_GetByID_NotFound(t *testing.T) {
    db, mock, _ := sqlmock.New()
    gdb, _ := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}))

    mock.ExpectQuery(`SELECT \* FROM "t_fv_core_assets"`).
        WithArgs(99, 1).
        WillReturnError(gorm.ErrRecordNotFound)

    repo := gormrepo.NewAssetRepo(gdb)
    _, err := repo.GetByID(context.Background(), 99)
    require.ErrorIs(t, err, errs.NotFound)
}
```

> 完整业务规则参见 `docs/domain-model.md`。
> 表与字段命名见 `naming-conventions` skill 与 `docs/database-schema.md`。
