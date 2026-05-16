# Repository 模式（fin-vault 项目级，强制）

> 本 Skill 是 ai-rd-team 内置规范的**新增**项，无 builtin 同名覆盖。
> Repository 抽象是"本地起步、升级改动最小"目标的关键基石——业务逻辑只依赖接口，
> 切换 SQLite → PostgreSQL → TDSQL 时仅需替换 `internal/repository/gorm/` 实现。

## 适用场景

- 新增一个数据库实体（domain + 表）
- 写需要持久化的 service 方法
- 写跨表事务（如 buy → 写 Transaction + 重算 Holding）
- 加缓存（写 Cache 装饰器包装原 Repository）
- 写 Repository 单元测试 / Service 测试

## 核心原则

### 1. Repository 接口与 GORM 实现严格分离

```
internal/repository/
├── interfaces.go              ← 全部接口集中（也可按模块拆分）
├── gorm/
│   ├── asset_repo.go
│   ├── holding_repo.go
│   └── ...
└── cache/
    └── asset_repo_cache.go    ← 装饰器：先查缓存，未命中再查 DB
```

接口包**只**依赖 `context` + `internal/domain`。**不**依赖 `gorm.io/gorm`。

### 2. 接口设计三原则

- **粗粒度**：一个 entity 一个 Repository，方法数 5-12 个为宜（Create / Get / Update / Delete / List + 业务查询）
- **领域语言**：方法名用业务词，不要数据库词。`GetByCode` ✅、`SelectWhereCodeEq` ❌
- **零驱动泄漏**：接口签名零 `*gorm.DB` / `gorm.Tx` / `*sql.DB`。事务通过 `UnitOfWork` 抽象提供

### 3. UnitOfWork 抽象事务

事务跨多个 Repository 时，**绝不**让 service 拿 `*gorm.DB`。用 `UnitOfWork` 封装：

```go
// internal/repository/interfaces.go
type Tx interface {
    Assets() AssetRepository
    Holdings() HoldingRepository
    Transactions() TransactionRepository
    CostLots() CostLotRepository
    // ... 按需补
}

type UnitOfWork interface {
    WithTx(ctx context.Context, fn func(tx Tx) error) error
}
```

GORM 实现：

```go
// internal/repository/gorm/uow.go
type uow struct{ db *gorm.DB }

func NewUnitOfWork(db *gorm.DB) repository.UnitOfWork { return &uow{db: db} }

func (u *uow) WithTx(ctx context.Context, fn func(repository.Tx) error) error {
    return u.db.WithContext(ctx).Transaction(func(g *gorm.DB) error {
        return fn(&txAdapter{db: g})
    })
}

type txAdapter struct{ db *gorm.DB }

func (t *txAdapter) Assets() repository.AssetRepository { return &assetRepo{db: t.db} }
func (t *txAdapter) Holdings() repository.HoldingRepository { return &holdingRepo{db: t.db} }
// ...
```

service 看不到 `*gorm.DB`，只见 `Tx`。

### 4. List 方法用结构化 Option

```go
type ListOption struct {
    UserID     *uint64
    AssetType  *domain.AssetType
    PlatformID *uint64
    Keyword    string
    Offset     int
    Limit      int
    OrderBy    string  // "f_created_at DESC"（白名单）
}

type AssetRepository interface {
    List(ctx context.Context, opt ListOption) ([]*domain.Asset, int64, error)
    // ...
}
```

- 用指针表示"未指定"
- `Limit` 默认 20，强制 ≤ 200（避免误传 0 拉全表）
- `OrderBy` 在实现里走白名单校验，**不直接拼用户字符串**（防 SQL 注入）

### 5. 缓存用装饰器，不在 Repository 实现里掺杂

```go
// internal/repository/cache/asset_repo_cache.go
type assetRepoCache struct {
    inner repository.AssetRepository
    cache cache.CacheProvider
}

func NewAssetRepoCache(inner repository.AssetRepository, c cache.CacheProvider) repository.AssetRepository {
    return &assetRepoCache{inner: inner, cache: c}
}

func (r *assetRepoCache) GetByID(ctx context.Context, id uint64) (*domain.Asset, error) {
    key := fmt.Sprintf("asset:%d", id)
    var a domain.Asset
    if ok, _ := r.cache.GetJSON(ctx, key, &a); ok {
        return &a, nil
    }
    fresh, err := r.inner.GetByID(ctx, id)
    if err != nil { return nil, err }
    _ = r.cache.SetJSON(ctx, key, fresh, 5*time.Minute)
    return fresh, nil
}
```

GORM Repo 实现保持**单一职责**：只查 DB。缓存策略与失效在装饰器层管理。

### 6. 写操作必须 invalidate 缓存

写操作的装饰器同步删 key（或通过 `EventBus` 异步发失效事件）：

```go
func (r *assetRepoCache) Update(ctx context.Context, a *domain.Asset) error {
    if err := r.inner.Update(ctx, a); err != nil { return err }
    _ = r.cache.Delete(ctx, fmt.Sprintf("asset:%d", a.ID))
    return nil
}
```

### 7. 查询不返回 ORM 副作用

GORM 的 `Preload` / `Joins` 结果**绝不**带回 `gorm.Model` 字段。返回的是 domain 实体，前端 / service 看不到 `*gorm.DB` 残留。

## 常用模式

### 模式 A：典型接口定义（asset 模块）

```go
// internal/repository/interfaces.go
type AssetRepository interface {
    Create(ctx context.Context, a *domain.Asset) error
    GetByID(ctx context.Context, id uint64) (*domain.Asset, error)
    GetByCode(ctx context.Context, userID uint64, code string) (*domain.Asset, error)
    Update(ctx context.Context, a *domain.Asset) error
    SoftDelete(ctx context.Context, id uint64) error
    List(ctx context.Context, opt ListOption) ([]*domain.Asset, int64, error)
    ExistsByCode(ctx context.Context, userID uint64, code string) (bool, error)
}
```

### 模式 B：GORM 实现（asset_repo.go）

```go
// internal/repository/gorm/asset_repo.go
type assetRepo struct{ db *gorm.DB }

func NewAssetRepo(db *gorm.DB) repository.AssetRepository { return &assetRepo{db: db} }

func (r *assetRepo) Create(ctx context.Context, a *domain.Asset) error {
    if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
        if isDuplicateKeyErr(err) {
            return errs.AssetCodeDuplicated.Wrap(err)
        }
        return errs.DBConnLost.Wrap(err)
    }
    return nil
}

func (r *assetRepo) List(ctx context.Context, opt repository.ListOption) ([]*domain.Asset, int64, error) {
    q := r.db.WithContext(ctx).Model(&domain.Asset{})
    if opt.UserID != nil    { q = q.Where("f_user_id = ?", *opt.UserID) }
    if opt.AssetType != nil { q = q.Where("f_asset_type = ?", *opt.AssetType) }
    if opt.Keyword != ""    { q = q.Where("f_asset_name LIKE ?", "%"+opt.Keyword+"%") }

    var total int64
    if err := q.Count(&total).Error; err != nil {
        return nil, 0, errs.DBConnLost.Wrap(err)
    }

    if opt.OrderBy != "" && allowedOrderBy[opt.OrderBy] {
        q = q.Order(opt.OrderBy)
    } else {
        q = q.Order("f_id DESC")
    }
    limit := opt.Limit
    if limit <= 0 || limit > 200 { limit = 20 }

    var list []*domain.Asset
    if err := q.Offset(opt.Offset).Limit(limit).Find(&list).Error; err != nil {
        return nil, 0, errs.DBConnLost.Wrap(err)
    }
    return list, total, nil
}

var allowedOrderBy = map[string]bool{
    "f_created_at DESC": true,
    "f_created_at ASC":  true,
    "f_asset_name ASC":  true,
}
```

### 模式 C：Service 中使用 UnitOfWork

```go
type txnService struct {
    uow         repository.UnitOfWork
    holdingCalc *HoldingCalc
}

func (s *txnService) Buy(ctx context.Context, in BuyInput) (*domain.Transaction, error) {
    var out *domain.Transaction
    err := s.uow.WithTx(ctx, func(tx repository.Tx) error {
        h, err := tx.Holdings().GetOrInit(ctx, in.UserID, in.AssetID, in.PlatformID)
        if err != nil { return err }

        txn := s.holdingCalc.NewBuyTransaction(h, in)
        if err := tx.Transactions().Create(ctx, txn); err != nil { return err }

        s.holdingCalc.ApplyBuy(h, txn)
        if err := tx.Holdings().Upsert(ctx, h); err != nil { return err }

        out = txn
        return nil
    })
    return out, err
}
```

service 完全没见到 `*gorm.DB`，事务、回滚都由 `WithTx` 兜底。

### 模式 D：Mock Repository 用于 Service 测试

`mockery` 或手写都行，推荐 `mockery`：

```bash
mockery --name AssetRepository --dir internal/repository --output internal/repository/mocks
```

测试：

```go
func TestAssetService_Create_Duplicate(t *testing.T) {
    repo := mocks.NewAssetRepository(t)
    repo.On("ExistsByCode", mock.Anything, uint64(1), "F000001").Return(true, nil)

    svc := service.NewAssetService(repo, nil)
    _, err := svc.Create(context.Background(), service.CreateAssetInput{
        UserID: 1, AssetCode: "F000001",
    })
    require.ErrorIs(t, err, errs.AssetCodeDuplicated)
    repo.AssertExpectations(t)
}
```

### 模式 E：Repository 用 sqlmock 单测（不真起 DB）

```go
func TestAssetRepo_Create_Duplicate(t *testing.T) {
    sqlDB, mock, _ := sqlmock.New()
    db, _ := gorm.Open(mysql.New(mysql.Config{
        Conn: sqlDB, SkipInitializeWithVersion: true,
    }))

    mock.ExpectBegin()
    mock.ExpectExec(`INSERT INTO "t_fv_core_assets"`).
        WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
    mock.ExpectRollback()

    repo := gormrepo.NewAssetRepo(db)
    err := repo.Create(context.Background(), &domain.Asset{AssetCode: "F000001"})
    require.ErrorIs(t, err, errs.AssetCodeDuplicated)
}
```

## 禁止

| ❌ 反模式 | ✅ 正确做法 |
|---|---|
| service 包 `import "gorm.io/gorm"` | 只 import `internal/repository` 接口 |
| Repository 方法签名暴露 `*gorm.DB` / `gorm.Tx` | 用 `context.Context` + 自家 `Tx` 接口 |
| service 直接 `db.Begin()` 开事务 | `uow.WithTx(ctx, func(tx Tx) error {...})` |
| 一个 Repository 接口塞 30 个方法 | 拆分或用 ListOption 收敛 |
| `func List(ctx, userID, type, platform, kw, offset, limit, orderBy)` 长参数列 | 用 `ListOption` struct |
| `OrderBy` 直接拼用户输入 | 走白名单 map 校验 |
| Repository 方法名带 SQL 词（`Select` / `Update`） | 用领域词（`Get` / `List` / `Save`） |
| `db.Find(&assets, "asset_code LIKE ?", k)` 不带 ctx | 必须 `WithContext(ctx)` |
| 缓存逻辑写在 GORM Repo 里 | 用装饰器分层 |
| Repository 不翻译 GORM 错误 | 翻译为 `errs.NotFound` / `errs.Conflict` / `errs.DBConnLost` |
| 跨 Repo 写操作不开事务 | 必须 `WithTx` 包起来 |
| 返回 `*gorm.DB` 给上层链式调用 | Repository 接口零 ORM 类型 |

## 示例

### 完整的 UnitOfWork 接口与实现

```go
// internal/repository/interfaces.go
type Tx interface {
    Assets() AssetRepository
    Holdings() HoldingRepository
    Transactions() TransactionRepository
    CostLots() CostLotRepository
    Quotes() QuoteRepository
}

type UnitOfWork interface {
    WithTx(ctx context.Context, fn func(tx Tx) error) error
}
```

```go
// internal/repository/gorm/uow.go
type uow struct{ db *gorm.DB }

func NewUnitOfWork(db *gorm.DB) repository.UnitOfWork { return &uow{db: db} }

func (u *uow) WithTx(ctx context.Context, fn func(repository.Tx) error) error {
    return u.db.WithContext(ctx).Transaction(func(g *gorm.DB) error {
        return fn(&txAdapter{db: g})
    })
}

type txAdapter struct{ db *gorm.DB }

func (t *txAdapter) Assets() repository.AssetRepository           { return &assetRepo{db: t.db} }
func (t *txAdapter) Holdings() repository.HoldingRepository       { return &holdingRepo{db: t.db} }
func (t *txAdapter) Transactions() repository.TransactionRepository { return &txnRepo{db: t.db} }
func (t *txAdapter) CostLots() repository.CostLotRepository       { return &costLotRepo{db: t.db} }
func (t *txAdapter) Quotes() repository.QuoteRepository           { return &quoteRepo{db: t.db} }
```

### Bootstrap Wire 装配

```go
// internal/bootstrap/wire.go
func ProvideAssetRepository(db *gorm.DB, cache cache.CacheProvider) repository.AssetRepository {
    inner := gormrepo.NewAssetRepo(db)
    return cacherepo.NewAssetRepoCache(inner, cache) // 装饰器
}

func ProvideTxnService(uow repository.UnitOfWork, calc *service.HoldingCalc) service.TxnService {
    return service.NewTxnService(uow, calc)
}
```

切换 SQLite → PostgreSQL：仅改 `bootstrap.LoadDB()` 里的 driver，业务代码零改动。
切换 Redis → 内存 Cache：仅改 `ProvideCache()`，装饰器接口签名不变。

> 完整事务边界清单见 `agent.d/coding-conventions.md` §事务边界。
> 命名见 `naming-conventions` skill。
> 错误翻译见 `error-handling` skill。
