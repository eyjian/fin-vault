package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// AssetService —— 资产 + 子类型 detail 一体 CRUD
// =====================================================================

// AssetService 资产服务。
type AssetService struct {
	uow          repository.UnitOfWork
	assetRepo    repository.AssetRepository
	platformRepo repository.PlatformRepository
}

// NewAssetService 构造资产服务。
func NewAssetService(
	uow repository.UnitOfWork,
	assetRepo repository.AssetRepository,
	platformRepo repository.PlatformRepository,
) *AssetService {
	return &AssetService{
		uow:          uow,
		assetRepo:    assetRepo,
		platformRepo: platformRepo,
	}
}

// CreateAssetInput 创建资产入参。
//
// 按 AssetType 选填对应 detail：
//   - fund   → FundDetail
//   - stock  → StockDetail
//   - wealth → WealthDetail（必填）
//   - cash   → asset_code 必须形如 CASH-{platform_code}-{currency}
type CreateAssetInput struct {
	UserID           uint
	AssetCode        string
	Name             string
	AssetType        domain.AssetType
	Currency         string
	IssuerPlatformID *uint
	RiskLevel        string
	Status           string
	Remark           string

	FundDetail   *domain.FundDetail
	StockDetail  *domain.StockDetail
	WealthDetail *domain.WealthDetail
}

// Create 创建资产 + 对应 detail。事务保证一致性。
func (s *AssetService) Create(ctx context.Context, in CreateAssetInput) (*domain.Asset, error) {
	if in.UserID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("user_id required")
	}
	if !in.AssetType.IsValid() {
		return nil, errs.ErrAssetTypeInvalid
	}
	if in.AssetCode == "" || in.Name == "" {
		return nil, errs.ErrInvalidParam.WithMsg("asset_code/name required")
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.Status == "" {
		in.Status = domain.StatusActive
	}

	// cash 类型 code 格式校验
	if in.AssetType == domain.AssetTypeCash {
		if !strings.HasPrefix(in.AssetCode, "CASH-") || strings.Count(in.AssetCode, "-") < 2 {
			return nil, errs.ErrCashCodeInvalid
		}
	}

	// wealth 类型必须带 detail
	if in.AssetType == domain.AssetTypeWealth && in.WealthDetail == nil {
		return nil, errs.ErrWealthDetailMissing
	}

	// 防重：(user_id, asset_code, asset_type)
	if existing, err := s.assetRepo.GetByCode(ctx, in.UserID, in.AssetCode, in.AssetType); err == nil && existing != nil {
		return nil, errs.ErrAssetDuplicated
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, errs.ErrDB.WithCause(err)
	}

	a := &domain.Asset{
		UserID:           in.UserID,
		AssetCode:        in.AssetCode,
		Name:             in.Name,
		AssetType:        in.AssetType,
		Currency:         in.Currency,
		IssuerPlatformID: in.IssuerPlatformID,
		RiskLevel:        in.RiskLevel,
		Status:           in.Status,
		Remark:           in.Remark,
	}
	now := time.Now()
	a.CreatedAt = now
	a.UpdatedAt = now

	err := s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.assetRepo.Create(ctx, a); err != nil {
			return errs.ErrDB.WithCause(err)
		}
		switch in.AssetType {
		case domain.AssetTypeFund:
			if in.FundDetail != nil {
				in.FundDetail.AssetID = a.ID
				if err := s.assetRepo.UpsertFundDetail(ctx, in.FundDetail); err != nil {
					return errs.ErrDB.WithCause(err)
				}
			}
		case domain.AssetTypeStock:
			if in.StockDetail != nil {
				in.StockDetail.AssetID = a.ID
				if err := s.assetRepo.UpsertStockDetail(ctx, in.StockDetail); err != nil {
					return errs.ErrDB.WithCause(err)
				}
			}
		case domain.AssetTypeWealth:
			in.WealthDetail.AssetID = a.ID
			if err := s.assetRepo.UpsertWealthDetail(ctx, in.WealthDetail); err != nil {
				return errs.ErrDB.WithCause(err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 重新拉一次以带回 detail
	full, err := s.assetRepo.GetByID(ctx, in.UserID, a.ID)
	if err != nil {
		return a, nil
	}
	return full, nil
}

// UpdateAssetInput 更新资产入参。
type UpdateAssetInput struct {
	UserID           uint
	ID               uint
	Name             string
	Currency         string
	IssuerPlatformID *uint
	RiskLevel        string
	Status           string
	Remark           string

	FundDetail   *domain.FundDetail
	StockDetail  *domain.StockDetail
	WealthDetail *domain.WealthDetail
}

// Update 更新资产主表 + 对应 detail。
func (s *AssetService) Update(ctx context.Context, in UpdateAssetInput) (*domain.Asset, error) {
	if in.UserID == 0 || in.ID == 0 {
		return nil, errs.ErrInvalidParam.WithMsg("user_id/id required")
	}
	a, err := s.assetRepo.GetByID(ctx, in.UserID, in.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errs.ErrAssetNotFound
		}
		return nil, errs.ErrDB.WithCause(err)
	}
	if in.Name != "" {
		a.Name = in.Name
	}
	if in.Currency != "" {
		a.Currency = in.Currency
	}
	if in.IssuerPlatformID != nil {
		a.IssuerPlatformID = in.IssuerPlatformID
	}
	a.RiskLevel = in.RiskLevel
	if in.Status != "" {
		a.Status = in.Status
	}
	a.Remark = in.Remark
	a.UpdatedAt = time.Now()

	err = s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.assetRepo.Update(ctx, a); err != nil {
			return errs.ErrDB.WithCause(err)
		}
		switch a.AssetType {
		case domain.AssetTypeFund:
			if in.FundDetail != nil {
				in.FundDetail.AssetID = a.ID
				if err := s.assetRepo.UpsertFundDetail(ctx, in.FundDetail); err != nil {
					return errs.ErrDB.WithCause(err)
				}
			}
		case domain.AssetTypeStock:
			if in.StockDetail != nil {
				in.StockDetail.AssetID = a.ID
				if err := s.assetRepo.UpsertStockDetail(ctx, in.StockDetail); err != nil {
					return errs.ErrDB.WithCause(err)
				}
			}
		case domain.AssetTypeWealth:
			if in.WealthDetail != nil {
				in.WealthDetail.AssetID = a.ID
				if err := s.assetRepo.UpsertWealthDetail(ctx, in.WealthDetail); err != nil {
					return errs.ErrDB.WithCause(err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.assetRepo.GetByID(ctx, in.UserID, in.ID)
}

// Delete 软删资产。
func (s *AssetService) Delete(ctx context.Context, userID, id uint) error {
	if userID == 0 || id == 0 {
		return errs.ErrInvalidParam.WithMsg("user_id/id required")
	}
	if err := s.assetRepo.Delete(ctx, userID, id); err != nil {
		return errs.ErrDB.WithCause(err)
	}
	return nil
}

// Get 取资产详情（含 detail Preload）。
func (s *AssetService) Get(ctx context.Context, userID, id uint) (*domain.Asset, error) {
	a, err := s.assetRepo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errs.ErrAssetNotFound
		}
		return nil, errs.ErrDB.WithCause(err)
	}
	return a, nil
}

// AssetListInput 列表查询入参。
type AssetListInput struct {
	UserID    uint
	AssetType domain.AssetType
	Status    string
	Currency  string
	Keyword   string
	Page      int
	PageSize  int
}

// List 列出资产。
func (s *AssetService) List(ctx context.Context, in AssetListInput) ([]domain.Asset, int64, error) {
	opts := repository.ListOptions{
		UserID:   in.UserID,
		Page:     in.Page,
		PageSize: in.PageSize,
		Filters:  map[string]any{},
	}
	if in.AssetType != "" {
		opts.Filters["asset_type"] = string(in.AssetType)
	}
	if in.Status != "" {
		opts.Filters["status"] = in.Status
	}
	if in.Currency != "" {
		opts.Filters["currency"] = in.Currency
	}
	if in.Keyword != "" {
		opts.Filters["keyword"] = in.Keyword
	}
	list, total, err := s.assetRepo.List(ctx, opts)
	if err != nil {
		return nil, 0, errs.ErrDB.WithCause(err)
	}
	return list, total, nil
}

// =====================================================================
// 平台字典只读视图（供 meta handler 使用）
// =====================================================================

// ListPlatforms 返回全部平台字典。
func (s *AssetService) ListPlatforms(ctx context.Context) ([]domain.Platform, error) {
	list, err := s.platformRepo.List(ctx)
	if err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}
	return list, nil
}
