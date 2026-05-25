// Package repository 定义 FinVault 持久层抽象接口（与 docs/design/ARCHITECTURE.md §6.1 对齐）。
//
// service 层只依赖本包接口，不直接依赖 GORM/SQL，便于切换实现与单元测试。
package repository

import (
	"context"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/domain"
)

// =====================================================================
// 通用查询参数
// =====================================================================

// ListOptions 通用列表查询参数。
//
// Filters 用于按字段精确过滤，例如 {"asset_type":"fund","status":"active"}。
// 各 Repository 实现按需消费 Filters 中的具体 key，未识别的 key 应当忽略。
type ListOptions struct {
	UserID   uint
	Page     int
	PageSize int
	OrderBy  string // 例如 "f_created_at desc"
	Filters  map[string]any
}

// Offset 计算 SQL OFFSET。
func (o ListOptions) Offset() int {
	if o.Page <= 0 {
		o.Page = 1
	}
	return (o.Page - 1) * o.Limit()
}

// Limit 计算每页大小（默认 20，上限 200）。
func (o ListOptions) Limit() int {
	if o.PageSize <= 0 {
		return 20
	}
	if o.PageSize > 200 {
		return 200
	}
	return o.PageSize
}

// =====================================================================
// UnitOfWork —— 事务边界抽象
// =====================================================================

// UnitOfWork 是数据库事务的抽象。Service 层通过 Do 来组合多个 Repository 操作，
// 保证一致性。**事务始终在 Service 层发起**，gorm 实现仅负责把 *gorm.DB 注入 ctx。
type UnitOfWork interface {
	// Do 在一次事务中执行 fn。fn 内可继续使用同一个 ctx 调用任意 Repository 方法，
	// 由 Repository 实现自动从 ctx 取出事务版连接。
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// =====================================================================
// User
// =====================================================================

// UserRepository 用户仓储。
type UserRepository interface {
	GetByID(ctx context.Context, id uint) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	Create(ctx context.Context, user *domain.User) error
	Update(ctx context.Context, user *domain.User) error
}

// =====================================================================
// Platform 字典
// =====================================================================

// PlatformRepository 平台字典仓储。
type PlatformRepository interface {
	List(ctx context.Context) ([]domain.Platform, error)
	GetByID(ctx context.Context, id uint) (*domain.Platform, error)
	GetByCode(ctx context.Context, code string) (*domain.Platform, error)
	Create(ctx context.Context, p *domain.Platform) error
	Update(ctx context.Context, p *domain.Platform) error
	Delete(ctx context.Context, id uint) error
}

// =====================================================================
// Asset
// =====================================================================

// AssetRepository 资产仓储。
//
// 关于 detail 子表：1:1 关联 Asset，使用 Upsert 语义。
// GetXxxDetail 在记录不存在时返回 (nil, nil) —— 业务上"detail 缺失"是常见场景，
// service 层通过 nil 判空，避免到处 errors.Is(err, ErrNotFound)。
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
	GetFundDetail(ctx context.Context, assetID uint) (*domain.FundDetail, error)
	GetStockDetail(ctx context.Context, assetID uint) (*domain.StockDetail, error)
	GetWealthDetail(ctx context.Context, assetID uint) (*domain.WealthDetail, error)
}

// =====================================================================
// Holding
// =====================================================================

// HoldingRepository 持仓仓储。
type HoldingRepository interface {
	Create(ctx context.Context, h *domain.Holding) error
	Update(ctx context.Context, h *domain.Holding) error
	GetByID(ctx context.Context, userID, id uint) (*domain.Holding, error)

	// GetOrCreate 在 (userID, assetID, platformID) 唯一键基础上原子获取或创建。
	// 用于交易写入前先确保 Holding 存在，是事务关键入口。
	GetOrCreate(ctx context.Context, userID, assetID, platformID uint) (*domain.Holding, error)

	ListByUser(ctx context.Context, opts ListOptions) ([]domain.Holding, int64, error)

	// ListMaturedWealth 列出指定日期之前已到期但状态仍为 holding 的理财持仓，
	// 供 MatureService 定时扫描使用。返回值为值切片（service 拿到后按需取地址）。
	ListMaturedWealth(ctx context.Context, asOfDate time.Time) ([]domain.Holding, error)
}

// =====================================================================
// Transaction
// =====================================================================

// TransactionRepository 交易流水仓储。
type TransactionRepository interface {
	Create(ctx context.Context, t *domain.Transaction) error
	GetByID(ctx context.Context, userID, id uint) (*domain.Transaction, error)
	List(ctx context.Context, opts ListOptions) ([]domain.Transaction, int64, error)
	ListByHolding(ctx context.Context, holdingID uint) ([]domain.Transaction, error)

	// ExistsByExternalID 校验外部订单号是否已经导入过，用于 CSV / 接口导入防重。
	// externalID 为空时直接返回 (false, nil)。
	ExistsByExternalID(ctx context.Context, userID, platformID uint, externalID string) (bool, error)
}

// =====================================================================
// CostLot（FIFO 辅助）
// =====================================================================

// CostLotRepository 成本批次仓储。
type CostLotRepository interface {
	Create(ctx context.Context, lot *domain.CostLot) error
	ListOpenByHolding(ctx context.Context, holdingID uint) ([]*domain.CostLot, error)
	Update(ctx context.Context, lot *domain.CostLot) error
	DeleteByHolding(ctx context.Context, holdingID uint) error
}

// =====================================================================
// Portfolio
// =====================================================================

// PortfolioRepository 投资组合仓储（一阶段建表，UI 暂未开放）。
type PortfolioRepository interface {
	GetByID(ctx context.Context, id uint) (*domain.Portfolio, error)
	ListByUser(ctx context.Context, userID uint) ([]*domain.Portfolio, error)
	Create(ctx context.Context, p *domain.Portfolio) error
	Update(ctx context.Context, p *domain.Portfolio) error
	Delete(ctx context.Context, id uint) error
}

// =====================================================================
// Quote / Rate（dev_2 行情/汇率服务也会 import 本接口）
// =====================================================================

// QuoteRepository 行情快照仓储。
type QuoteRepository interface {
	Insert(ctx context.Context, q *domain.PriceQuote) error
	GetLatest(ctx context.Context, assetID uint) (*domain.PriceQuote, error)
	BatchGetLatest(ctx context.Context, assetIDs []uint) (map[uint]*domain.PriceQuote, error)
	ListHistory(ctx context.Context, assetID uint, from, to time.Time) ([]*domain.PriceQuote, error)
}

// RateRepository 汇率快照仓储。
type RateRepository interface {
	Insert(ctx context.Context, r *domain.ExchangeRate) error
	GetLatest(ctx context.Context, from, to string, asOf time.Time) (*domain.ExchangeRate, error)
	List(ctx context.Context, from, to string, fromDate, toDate time.Time) ([]*domain.ExchangeRate, error)
}

// =====================================================================
// PulseDiagnosis（AI 把脉结果）
// =====================================================================

// PulseDiagnosisRepository AI 把脉结果仓储。
//
// 唯一约束 (UserID, AssetID)：每个用户的每个资产只保留最新一条把脉结果，
// Upsert 由 ON CONFLICT 保证；GetByUserAsset 在记录不存在时返回 (nil, nil)
// —— 业务上"还未把脉"是常见场景，调用方通过 nil 判空，避免 errors.Is(ErrNotFound)。
type PulseDiagnosisRepository interface {
	// Upsert 创建或更新把脉结果。命中 (UserID, AssetID) 时覆盖
	// Recommendation/Confidence/Summary/Detail/DataReferences/RawResponse/SessionID/TriggerSource/UpdatedAt，
	// 保留 CreatedAt 与 ID 不变。
	Upsert(ctx context.Context, d *domain.PulseDiagnosis) error

	// GetByUserAsset 取单个 (UserID, AssetID) 的最新把脉结果；
	// 不存在返回 (nil, nil)。
	GetByUserAsset(ctx context.Context, userID, assetID uint) (*domain.PulseDiagnosis, error)

	// ListByUser 列出用户的把脉结果（按 UpdatedAt 倒序）。
	// 当 assetIDs 非空时，仅返回这些资产对应的记录（用于资产管理页批量预加载）。
	ListByUser(ctx context.Context, userID uint, assetIDs []uint) ([]domain.PulseDiagnosis, error)
}

// =====================================================================
// Repositories 聚合（供 Wire 一次性注入到 Service）
// =====================================================================

// Repositories 聚合所有仓储，方便 bootstrap.Wire 一次性注入。
type Repositories struct {
	UoW            UnitOfWork
	User           UserRepository
	Platform       PlatformRepository
	Asset          AssetRepository
	Holding        HoldingRepository
	Transaction    TransactionRepository
	CostLot        CostLotRepository
	Portfolio      PortfolioRepository
	Quote          QuoteRepository
	Rate           RateRepository
	PulseDiagnosis PulseDiagnosisRepository
	SysConfig      SysConfigRepository
}
