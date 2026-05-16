package gormrepo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

type assetRepo struct{ db *gorm.DB }

// NewAssetRepository 构造 AssetRepository。
func NewAssetRepository(db *gorm.DB) repository.AssetRepository {
	return &assetRepo{db: db}
}

func (r *assetRepo) Create(ctx context.Context, a *domain.Asset) error {
	return dbFrom(ctx, r.db).Create(a).Error
}

func (r *assetRepo) Update(ctx context.Context, a *domain.Asset) error {
	return dbFrom(ctx, r.db).Model(&domain.Asset{}).
		Where("f_id = ? AND f_user_id = ?", a.ID, a.UserID).
		Updates(map[string]interface{}{
			"f_name":               a.Name,
			"f_currency":           a.Currency,
			"f_issuer_platform_id": a.IssuerPlatformID,
			"f_risk_level":         a.RiskLevel,
			"f_status":             a.Status,
			"f_remark":             a.Remark,
			"f_updated_at":         time.Now(),
		}).Error
}

func (r *assetRepo) GetByID(ctx context.Context, userID, id uint) (*domain.Asset, error) {
	var a domain.Asset
	q := dbFrom(ctx, r.db).
		Preload("FundDetail").Preload("StockDetail").Preload("WealthDetail")
	if userID > 0 {
		q = q.Where("f_user_id = ?", userID)
	}
	if err := q.First(&a, id).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &a, nil
}

func (r *assetRepo) GetByCode(ctx context.Context, userID uint, code string, t domain.AssetType) (*domain.Asset, error) {
	var a domain.Asset
	if err := dbFrom(ctx, r.db).
		Where("f_user_id = ? AND f_asset_code = ? AND f_asset_type = ?", userID, code, t).
		First(&a).Error; err != nil {
		return nil, translateNotFound(err)
	}
	return &a, nil
}

func (r *assetRepo) List(ctx context.Context, opts repository.ListOptions) ([]domain.Asset, int64, error) {
	tx := dbFrom(ctx, r.db).Model(&domain.Asset{})
	if opts.UserID > 0 {
		tx = tx.Where("f_user_id = ?", opts.UserID)
	}
	for k, v := range opts.Filters {
		switch k {
		case "asset_type":
			tx = tx.Where("f_asset_type = ?", v)
		case "status":
			tx = tx.Where("f_status = ?", v)
		case "currency":
			tx = tx.Where("f_currency = ?", v)
		case "keyword":
			if s, ok := v.(string); ok && s != "" {
				kw := "%" + s + "%"
				tx = tx.Where("f_name LIKE ? OR f_asset_code LIKE ?", kw, kw)
			}
		}
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := opts.OrderBy
	if order == "" {
		order = "f_id desc"
	}
	var list []domain.Asset
	if err := tx.
		Preload("FundDetail").Preload("StockDetail").Preload("WealthDetail").
		Order(order).
		Offset(opts.Offset()).Limit(opts.Limit()).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *assetRepo) Delete(ctx context.Context, userID, id uint) error {
	return dbFrom(ctx, r.db).
		Where("f_user_id = ?", userID).
		Delete(&domain.Asset{}, id).Error
}

func (r *assetRepo) UpsertFundDetail(ctx context.Context, d *domain.FundDetail) error {
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	return dbFrom(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "f_asset_id"}},
		UpdateAll: true,
	}).Create(d).Error
}

func (r *assetRepo) UpsertStockDetail(ctx context.Context, d *domain.StockDetail) error {
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	return dbFrom(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "f_asset_id"}},
		UpdateAll: true,
	}).Create(d).Error
}

func (r *assetRepo) UpsertWealthDetail(ctx context.Context, d *domain.WealthDetail) error {
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	return dbFrom(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "f_asset_id"}},
		UpdateAll: true,
	}).Create(d).Error
}

// GetFundDetail 不存在时返回 (nil, nil)，与 service 层 nil 判空惯例对齐。
func (r *assetRepo) GetFundDetail(ctx context.Context, assetID uint) (*domain.FundDetail, error) {
	var d domain.FundDetail
	err := dbFrom(ctx, r.db).Where("f_asset_id = ?", assetID).First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// GetStockDetail 不存在时返回 (nil, nil)。
func (r *assetRepo) GetStockDetail(ctx context.Context, assetID uint) (*domain.StockDetail, error) {
	var d domain.StockDetail
	err := dbFrom(ctx, r.db).Where("f_asset_id = ?", assetID).First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// GetWealthDetail 不存在时返回 (nil, nil)。MatureService 依赖此语义判空。
func (r *assetRepo) GetWealthDetail(ctx context.Context, assetID uint) (*domain.WealthDetail, error) {
	var d domain.WealthDetail
	err := dbFrom(ctx, r.db).Where("f_asset_id = ?", assetID).First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}
