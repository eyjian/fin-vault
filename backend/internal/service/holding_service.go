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

// SummaryByType 按资产类型聚合行。
type SummaryByType struct {
	AssetType   string          `json:"asset_type"`
	MarketValue decimal.Decimal `json:"market_value"`
	Ratio       decimal.Decimal `json:"ratio"`
}

// SummaryByPlatform 按平台聚合行。
type SummaryByPlatform struct {
	PlatformID   uint            `json:"platform_id"`
	PlatformName string          `json:"platform_name"`
	MarketValue  decimal.Decimal `json:"market_value"`
	Ratio        decimal.Decimal `json:"ratio"`
}

// SummaryByCurrency 按币种聚合行。
type SummaryByCurrency struct {
	Currency    string          `json:"currency"`
	MarketValue decimal.Decimal `json:"market_value"`
	Ratio       decimal.Decimal `json:"ratio"`
}

// HoldingSummary 持仓总览：总计 + 三维聚合（type / platform / currency）。
//
// 注意：所有切片字段始终初始化为非 nil，避免 JSON 序列化为 null 导致前端 el-table 报
// "rows is not iterable"。
type HoldingSummary struct {
	DisplayCurrency  string              `json:"display_currency"`
	TotalMarketValue decimal.Decimal     `json:"total_market_value"`
	TotalCost        decimal.Decimal     `json:"total_cost"`
	TotalPnL         decimal.Decimal     `json:"total_pnl"`
	PnLRatio         decimal.Decimal     `json:"pnl_ratio"`
	ByType           []SummaryByType     `json:"by_type"`
	ByPlatform       []SummaryByPlatform `json:"by_platform"`
	ByCurrency       []SummaryByCurrency `json:"by_currency"`
}

// Summary 三维聚合：按 type / platform / currency 汇总市值，并附带总市值/成本/盈亏。
//
// displayCurrency: "raw"=原币种汇总（不折算，仅 by_currency 维度有意义）、
// "CNY"=统一折算到人民币。
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
		DisplayCurrency:  displayCurrency,
		TotalMarketValue: decimal.Zero,
		TotalCost:        decimal.Zero,
		TotalPnL:         decimal.Zero,
		PnLRatio:         decimal.Zero,
		ByType:           []SummaryByType{},
		ByPlatform:       []SummaryByPlatform{},
		ByCurrency:       []SummaryByCurrency{},
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

	// 中间累加 map：保持插入有序聚合，最后再展开成切片。
	typeAgg := map[string]decimal.Decimal{}
	typeOrder := make([]string, 0)
	platAggMV := map[uint]decimal.Decimal{}
	platOrder := make([]uint, 0)
	currAgg := map[string]decimal.Decimal{}
	currOrder := make([]string, 0)

	for i := range holdings {
		h := holdings[i]
		view := s.buildView(&h, quoteMap[h.AssetID])

		// 折算市值与成本到展示币种
		mv := view.MarketValue
		cost := h.TotalCost
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
			cost = cost.Mul(rate.Rate).Round(2)
		}

		// 总计累加
		summary.TotalMarketValue = summary.TotalMarketValue.Add(mv)
		summary.TotalCost = summary.TotalCost.Add(cost)

		// 按类型
		assetType := ""
		if h.Asset != nil {
			assetType = string(h.Asset.AssetType)
		}
		if _, ok := typeAgg[assetType]; !ok {
			typeOrder = append(typeOrder, assetType)
		}
		typeAgg[assetType] = typeAgg[assetType].Add(mv)

		// 按平台
		if _, ok := platAggMV[h.PlatformID]; !ok {
			platOrder = append(platOrder, h.PlatformID)
		}
		platAggMV[h.PlatformID] = platAggMV[h.PlatformID].Add(mv)

		// 按币种（用原币种维度，不受 displayCurrency 影响时也用 mv 累计折算后金额，便于 UI 占比展示）
		if _, ok := currAgg[curr]; !ok {
			currOrder = append(currOrder, curr)
		}
		currAgg[curr] = currAgg[curr].Add(mv)
	}

	// 总盈亏 = 总市值 - 总成本；收益率 = 总盈亏 / 总成本（成本为 0 时给 0）
	summary.TotalPnL = summary.TotalMarketValue.Sub(summary.TotalCost).Round(2)
	if !summary.TotalCost.IsZero() {
		summary.PnLRatio = summary.TotalPnL.Div(summary.TotalCost).Round(6)
	}

	// 展开聚合 map -> 切片，并计算 ratio（保留 4 位）。
	ratioOf := func(mv decimal.Decimal) decimal.Decimal {
		if summary.TotalMarketValue.IsZero() {
			return decimal.Zero
		}
		return mv.Div(summary.TotalMarketValue).Round(4)
	}
	for _, k := range typeOrder {
		summary.ByType = append(summary.ByType, SummaryByType{
			AssetType:   k,
			MarketValue: typeAgg[k],
			Ratio:       ratioOf(typeAgg[k]),
		})
	}
	for _, pid := range platOrder {
		name := platformName[pid]
		if name == "" {
			name = "unknown"
		}
		summary.ByPlatform = append(summary.ByPlatform, SummaryByPlatform{
			PlatformID:   pid,
			PlatformName: name,
			MarketValue:  platAggMV[pid],
			Ratio:        ratioOf(platAggMV[pid]),
		})
	}
	for _, k := range currOrder {
		summary.ByCurrency = append(summary.ByCurrency, SummaryByCurrency{
			Currency:    k,
			MarketValue: currAgg[k],
			Ratio:       ratioOf(currAgg[k]),
		})
	}

	// 各维度按市值降序排序，UI 展示更直观
	sort.Slice(summary.ByType, func(i, j int) bool {
		return summary.ByType[i].MarketValue.GreaterThan(summary.ByType[j].MarketValue)
	})
	sort.Slice(summary.ByPlatform, func(i, j int) bool {
		return summary.ByPlatform[i].MarketValue.GreaterThan(summary.ByPlatform[j].MarketValue)
	})
	sort.Slice(summary.ByCurrency, func(i, j int) bool {
		return summary.ByCurrency[i].MarketValue.GreaterThan(summary.ByCurrency[j].MarketValue)
	})

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
