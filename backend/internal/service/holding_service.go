package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// HoldingService —— 持仓视图 / 聚合统计
// =====================================================================

// HoldingService 持仓服务。
type HoldingService struct {
	holdingRepo  repository.HoldingRepository
	assetRepo    repository.AssetRepository
	quoteRepo    repository.QuoteRepository
	rateRepo     repository.RateRepository
	platformRepo repository.PlatformRepository
}

// NewHoldingService 构造。
func NewHoldingService(
	holdingRepo repository.HoldingRepository,
	assetRepo repository.AssetRepository,
	quoteRepo repository.QuoteRepository,
	rateRepo repository.RateRepository,
	platformRepo repository.PlatformRepository,
) *HoldingService {
	return &HoldingService{
		holdingRepo:  holdingRepo,
		assetRepo:    assetRepo,
		quoteRepo:    quoteRepo,
		rateRepo:     rateRepo,
		platformRepo: platformRepo,
	}
}

// HoldingListInput 列表入参。
type HoldingListInput struct {
	UserID          uint
	AssetID         uint
	PlatformID      uint
	PortfolioID     uint
	Status          domain.HoldingStatus
	AssetType       domain.AssetType
	Page            int
	PageSize        int
	DisplayCurrency string // raw / CNY
}

// List 持仓列表，含市值/盈亏实时计算。
func (s *HoldingService) List(ctx context.Context, in HoldingListInput) ([]domain.HoldingView, int64, error) {
	opts := repository.ListOptions{
		UserID:   in.UserID,
		Page:     in.Page,
		PageSize: in.PageSize,
		Filters:  map[string]any{},
	}
	if in.AssetID > 0 {
		opts.Filters["asset_id"] = in.AssetID
	}
	if in.PlatformID > 0 {
		opts.Filters["platform_id"] = in.PlatformID
	}
	if in.PortfolioID > 0 {
		opts.Filters["portfolio_id"] = in.PortfolioID
	}
	if in.Status != "" {
		opts.Filters["status"] = string(in.Status)
	}
	if in.AssetType != "" {
		opts.Filters["asset_type"] = string(in.AssetType)
	}
	holdings, total, err := s.holdingRepo.ListByUser(ctx, opts)
	if err != nil {
		return nil, 0, errs.ErrDB.WithCause(err)
	}
	if len(holdings) == 0 {
		return nil, 0, nil
	}

	// 批量取最新价
	assetIDs := make([]uint, 0, len(holdings))
	for _, h := range holdings {
		assetIDs = append(assetIDs, h.AssetID)
	}
	quoteMap, _ := s.quoteRepo.BatchGetLatest(ctx, assetIDs)

	views := make([]domain.HoldingView, 0, len(holdings))
	for i := range holdings {
		h := holdings[i]
		view := s.buildView(&h, quoteMap[h.AssetID])
		// CNY 折算
		if in.DisplayCurrency == "CNY" && h.Asset != nil && h.Asset.Currency != "CNY" && h.Asset.Currency != "" {
			rate, err := s.rateRepo.GetLatest(ctx, h.Asset.Currency, "CNY", time.Now())
			if err == nil && rate != nil {
				view.MarketValueCNY = view.MarketValue.Mul(rate.Rate).Round(2)
			}
		} else if in.DisplayCurrency == "CNY" {
			view.MarketValueCNY = view.MarketValue
		}
		views = append(views, view)
	}
	return views, total, nil
}

// Get 取单条持仓视图。
func (s *HoldingService) Get(ctx context.Context, userID, id uint) (*domain.HoldingView, error) {
	h, err := s.holdingRepo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errs.ErrHoldingNotFound
		}
		return nil, errs.ErrDB.WithCause(err)
	}
	q, _ := s.quoteRepo.GetLatest(ctx, h.AssetID)
	view := s.buildView(h, q)
	return &view, nil
}

// SwitchCostMethod 切换持仓成本计算方法（weighted_avg ↔ fifo）。
//
// 切换到 fifo 时由调用方触发 CostLot 重建（D1-06 任务）。
func (s *HoldingService) SwitchCostMethod(ctx context.Context, userID, id uint, method domain.CostMethod) error {
	if method != domain.CostMethodWeightedAvg && method != domain.CostMethodFIFO {
		return errs.ErrInvalidParam.WithMsg("invalid cost_method")
	}
	h, err := s.holdingRepo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errs.ErrHoldingNotFound
		}
		return errs.ErrDB.WithCause(err)
	}
	h.CostMethod = method
	h.UpdatedAt = time.Now()
	return s.holdingRepo.Update(ctx, h)
}

// HoldingSummary 三维聚合（type / platform / currency）。
type HoldingSummary struct {
	ByType     map[string]decimal.Decimal `json:"by_type"`
	ByPlatform map[string]decimal.Decimal `json:"by_platform"`
	ByCurrency map[string]decimal.Decimal `json:"by_currency"`
	Total      decimal.Decimal            `json:"total"`
	Currency   string                     `json:"currency"`
}

// Summary 三维聚合：按 type / platform / currency 汇总市值。
//
// displayCurrency: "raw"=原币种汇总、"CNY"=统一折算到人民币。
func (s *HoldingService) Summary(ctx context.Context, userID uint, displayCurrency string) (*HoldingSummary, error) {
	holdings, _, err := s.holdingRepo.ListByUser(ctx, repository.ListOptions{
		UserID:   userID,
		Page:     1,
		PageSize: 1000,
		Filters:  map[string]any{"status": string(domain.HoldingStatusHolding)},
	})
	if err != nil {
		return nil, errs.ErrDB.WithCause(err)
	}
	summary := &HoldingSummary{
		ByType:     map[string]decimal.Decimal{},
		ByPlatform: map[string]decimal.Decimal{},
		ByCurrency: map[string]decimal.Decimal{},
		Total:      decimal.Zero,
		Currency:   displayCurrency,
	}
	if len(holdings) == 0 {
		return summary, nil
	}

	assetIDs := make([]uint, 0, len(holdings))
	for _, h := range holdings {
		assetIDs = append(assetIDs, h.AssetID)
	}
	quoteMap, _ := s.quoteRepo.BatchGetLatest(ctx, assetIDs)
	platforms, _ := s.platformRepo.List(ctx)
	platformName := make(map[uint]string, len(platforms))
	for _, p := range platforms {
		platformName[p.ID] = p.Name
	}

	for i := range holdings {
		h := holdings[i]
		view := s.buildView(&h, quoteMap[h.AssetID])

		// 折算
		mv := view.MarketValue
		curr := "CNY"
		if h.Asset != nil {
			curr = h.Asset.Currency
		}
		if displayCurrency == "CNY" && curr != "CNY" && curr != "" {
			rate, err := s.rateRepo.GetLatest(ctx, curr, "CNY", time.Now())
			if err != nil || rate == nil {
				return nil, errs.ErrExchangeRateNotFound.WithMsg("missing rate for " + curr + "->CNY")
			}
			mv = mv.Mul(rate.Rate).Round(2)
		}

		// 汇总
		assetType := ""
		if h.Asset != nil {
			assetType = string(h.Asset.AssetType)
		}
		summary.ByType[assetType] = summary.ByType[assetType].Add(mv)
		pname := platformName[h.PlatformID]
		if pname == "" {
			pname = "unknown"
		}
		summary.ByPlatform[pname] = summary.ByPlatform[pname].Add(mv)
		summary.ByCurrency[curr] = summary.ByCurrency[curr].Add(mv)
		summary.Total = summary.Total.Add(mv)
	}
	return summary, nil
}

// =====================================================================
// 内部辅助
// =====================================================================

// buildView 用最新行情计算视图字段。
func (s *HoldingService) buildView(h *domain.Holding, q *domain.PriceQuote) domain.HoldingView {
	view := domain.HoldingView{Holding: h}
	if q != nil {
		view.LatestPrice = q.Price
	}
	view.MarketValue = h.Quantity.Mul(view.LatestPrice).Round(2)
	view.UnrealizedPnL = view.MarketValue.Sub(h.TotalCost).Round(2)
	view.TotalPnL = view.UnrealizedPnL.Add(h.RealizedPnL).Add(h.TotalDividend).Round(2)
	if !h.TotalCost.IsZero() {
		view.PnLRatio = view.TotalPnL.Div(h.TotalCost).Round(6)
	}
	return view
}

// 编译期反向断言：保证 sort.Slice 在某些场景使用（部分 IDE 容易误报未使用导入）
var _ = sort.Slice
