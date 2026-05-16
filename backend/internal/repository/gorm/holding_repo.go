package gormrepo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

type holdingRepo struct{ db *gorm.DB }

// NewHoldingRepository 构造 HoldingRepository。
func NewHoldingRepository(db *gorm.DB) repository.HoldingRepository {
	return &holdingRepo{db: db}
}

func (r *holdingRepo) Create(ctx context.Context, h *domain.Holding) error {
	return dbFrom(ctx, r.db).Create(h).Error
}

func (r *holdingRepo) Update(ctx context.Context, h *domain.Holding) error {
	return dbFrom(ctx, r.db).Save(h).Error
}

func (r *holdingRepo) GetByID(ctx context.Context, userID, id uint) (*domain.Holding, error) {
	var h domain.Holding
	q := dbFrom(ctx, r.db)
	if userID > 0 {
		q = q.Where("f_user_id = ?", userID)
	}
	if err := q.First(&h, id).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &h, nil
}

// GetOrCreate 通过 (userID, assetID, platformID) 唯一键原子获取或创建。
//
// 实现策略：先 First，若 NotFound 则 Create；并发场景由唯一索引兜底（再次 First）。
func (r *holdingRepo) GetOrCreate(ctx context.Context, userID, assetID, platformID uint) (*domain.Holding, error) {
	var h domain.Holding
	tx := dbFrom(ctx, r.db).
		Where("f_user_id = ? AND f_asset_id = ? AND f_platform_id = ?", userID, assetID, platformID)
	err := tx.First(&h).Error
	if err == nil {
		return &h, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := time.Now()
	h = domain.Holding{
		UserID:     userID,
		AssetID:    assetID,
		PlatformID: platformID,
		CostMethod: domain.CostMethodWeightedAvg,
		Status:     domain.HoldingStatusHolding,
	}
	h.CreatedAt = now
	h.UpdatedAt = now
	if err := dbFrom(ctx, r.db).Create(&h).Error; err != nil {
		// 并发冲突：再读一次
		if err2 := tx.First(&h).Error; err2 == nil {
			return &h, nil
		}
		return nil, err
	}
	return &h, nil
}

func (r *holdingRepo) ListByUser(ctx context.Context, opts repository.ListOptions) ([]domain.Holding, int64, error) {
	tx := dbFrom(ctx, r.db).Model(&domain.Holding{})
	if opts.UserID > 0 {
		tx = tx.Where("t_fv_core_holdings.f_user_id = ?", opts.UserID)
	}
	for k, v := range opts.Filters {
		switch k {
		case "asset_id":
			tx = tx.Where("t_fv_core_holdings.f_asset_id = ?", v)
		case "platform_id":
			tx = tx.Where("t_fv_core_holdings.f_platform_id = ?", v)
		case "portfolio_id":
			tx = tx.Where("t_fv_core_holdings.f_portfolio_id = ?", v)
		case "status":
			tx = tx.Where("t_fv_core_holdings.f_status = ?", v)
		case "asset_type":
			tx = tx.Joins("JOIN t_fv_core_assets a ON a.f_id = t_fv_core_holdings.f_asset_id").
				Where("a.f_asset_type = ?", v)
		}
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := opts.OrderBy
	if order == "" {
		order = "t_fv_core_holdings.f_id desc"
	}
	var list []domain.Holding
	if err := tx.
		Preload("Asset").Preload("Asset.FundDetail").Preload("Asset.StockDetail").Preload("Asset.WealthDetail").
		Preload("Platform").
		Order(order).
		Offset(opts.Offset()).Limit(opts.Limit()).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListMaturedWealth 列出指定日期之前到期、状态仍为 holding 的理财持仓。
//
// JOIN：t_fv_core_holdings → t_fv_core_assets → t_fv_core_wealth_details
func (r *holdingRepo) ListMaturedWealth(ctx context.Context, asOfDate time.Time) ([]domain.Holding, error) {
	var list []domain.Holding
	err := dbFrom(ctx, r.db).Model(&domain.Holding{}).
		Joins("JOIN t_fv_core_assets a ON a.f_id = t_fv_core_holdings.f_asset_id").
		Joins("JOIN t_fv_core_wealth_details w ON w.f_asset_id = a.f_id").
		Where("a.f_asset_type = ?", domain.AssetTypeWealth).
		Where("t_fv_core_holdings.f_status = ?", domain.HoldingStatusHolding).
		Where("w.f_end_date IS NOT NULL AND w.f_end_date <= ?", asOfDate).
		Order("t_fv_core_holdings.f_id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// =====================================================================
// CostLot
// =====================================================================

type costLotRepo struct{ db *gorm.DB }

// NewCostLotRepository 构造 CostLotRepository。
func NewCostLotRepository(db *gorm.DB) repository.CostLotRepository {
	return &costLotRepo{db: db}
}

func (r *costLotRepo) Create(ctx context.Context, lot *domain.CostLot) error {
	return dbFrom(ctx, r.db).Create(lot).Error
}

func (r *costLotRepo) ListOpenByHolding(ctx context.Context, holdingID uint) ([]*domain.CostLot, error) {
	var list []*domain.CostLot
	if err := dbFrom(ctx, r.db).
		Where("f_holding_id = ? AND f_status = ?", holdingID, "open").
		Order("f_buy_time ASC, f_id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *costLotRepo) Update(ctx context.Context, lot *domain.CostLot) error {
	return dbFrom(ctx, r.db).Save(lot).Error
}

func (r *costLotRepo) DeleteByHolding(ctx context.Context, holdingID uint) error {
	return dbFrom(ctx, r.db).
		Where("f_holding_id = ?", holdingID).
		Delete(&domain.CostLot{}).Error
}

// =====================================================================
// Portfolio
// =====================================================================

type portfolioRepo struct{ db *gorm.DB }

// NewPortfolioRepository 构造 PortfolioRepository。
func NewPortfolioRepository(db *gorm.DB) repository.PortfolioRepository {
	return &portfolioRepo{db: db}
}

func (r *portfolioRepo) GetByID(ctx context.Context, id uint) (*domain.Portfolio, error) {
	var p domain.Portfolio
	if err := dbFrom(ctx, r.db).First(&p, id).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &p, nil
}

func (r *portfolioRepo) ListByUser(ctx context.Context, userID uint) ([]*domain.Portfolio, error) {
	var list []*domain.Portfolio
	if err := dbFrom(ctx, r.db).
		Where("f_user_id = ?", userID).
		Order("f_sort_order ASC, f_id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *portfolioRepo) Create(ctx context.Context, p *domain.Portfolio) error {
	return dbFrom(ctx, r.db).Create(p).Error
}

func (r *portfolioRepo) Update(ctx context.Context, p *domain.Portfolio) error {
	return dbFrom(ctx, r.db).Save(p).Error
}

func (r *portfolioRepo) Delete(ctx context.Context, id uint) error {
	return dbFrom(ctx, r.db).Delete(&domain.Portfolio{}, id).Error
}
