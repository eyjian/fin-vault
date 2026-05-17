package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/cache"
	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/platformapi"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// QuoteService —— 行情查询 + 主动刷新
// =====================================================================

// QuoteService 行情服务。
type QuoteService struct {
	repo       repository.QuoteRepository
	assetRepo  repository.AssetRepository
	cache      cache.Provider
	aggregator *platformapi.QuoteAggregator
	cacheTTL   time.Duration
}

// NewQuoteService 构造。cacheTTL<=0 默认 60s。
func NewQuoteService(
	repo repository.QuoteRepository,
	assetRepo repository.AssetRepository,
	c cache.Provider,
	agg *platformapi.QuoteAggregator,
	cacheTTL time.Duration,
) *QuoteService {
	if cacheTTL <= 0 {
		cacheTTL = 60 * time.Second
	}
	return &QuoteService{
		repo:       repo,
		assetRepo:  assetRepo,
		cache:      c,
		aggregator: agg,
		cacheTTL:   cacheTTL,
	}
}

// LatestQuote 返回给前端展示用的最新行情视图（命中缓存优先）。
type LatestQuote struct {
	AssetID   uint   `json:"asset_id"`
	Price     string `json:"price"`
	ChangePct string `json:"change_pct"`
	Volume    string `json:"volume"`
	QuoteTime string `json:"quote_time"`
	Source    string `json:"source"`
}

// GetLatest 批量取最新行情。优先走 cache，未命中再查 DB；不主动拉取第三方（用 Refresh）。
func (s *QuoteService) GetLatest(ctx context.Context, assetIDs []uint) ([]LatestQuote, error) {
	if len(assetIDs) == 0 {
		return []LatestQuote{}, nil
	}
	out := make([]LatestQuote, 0, len(assetIDs))
	missing := make([]uint, 0, len(assetIDs))

	for _, id := range assetIDs {
		key := cacheKeyQuote(id)
		if v, err := s.cache.Get(ctx, key); err == nil {
			var q LatestQuote
			if err := json.Unmarshal([]byte(v), &q); err == nil {
				out = append(out, q)
				continue
			}
		}
		missing = append(missing, id)
	}

	if len(missing) > 0 {
		quotes, err := s.repo.BatchGetLatest(ctx, missing)
		if err != nil {
			return nil, err
		}
		for _, id := range missing {
			q, ok := quotes[id]
			if !ok || q == nil {
				continue
			}
			lq := toLatest(q)
			out = append(out, lq)
			if b, err := json.Marshal(lq); err == nil {
				_ = s.cache.Set(ctx, cacheKeyQuote(id), string(b), s.cacheTTL)
			}
		}
	}
	return out, nil
}

// Refresh 主动从第三方拉取并写入 PriceQuote。source=auto 时按优先级降级。
//
// 返回每个资产的处理结果，包括失败原因（以 40001 错误码体系报告）。
type RefreshResult struct {
	AssetID   uint   `json:"asset_id"`
	AssetCode string `json:"asset_code,omitempty"`
	Name      string `json:"name,omitempty"`
	Source    string `json:"source,omitempty"`
	Price     string `json:"price,omitempty"`
	OK        bool   `json:"ok"`
	Message   string `json:"message,omitempty"`
}

// Refresh 主动刷新。assetIDs 为空时全 fund/stock 资产。
func (s *QuoteService) Refresh(ctx context.Context, userID uint, assetIDs []uint, source string) ([]RefreshResult, error) {
	if s.aggregator == nil {
		return nil, errs.ErrQuoteFetchFailed.WithMsg("quote aggregator not configured")
	}
	// 解析待刷新资产为 platformapi.AssetKey
	assets := make([]*domain.Asset, 0, len(assetIDs))
	if len(assetIDs) == 0 {
		// 没有指定 ids 时拉取全部 active 的 fund + stock
		for _, t := range []domain.AssetType{domain.AssetTypeFund, domain.AssetTypeStock} {
			list, _, err := s.assetRepo.List(ctx, repository.ListOptions{
				UserID:   userID,
				Page:     1,
				PageSize: 500,
				Filters:  map[string]any{"asset_type": string(t), "status": "active"},
			})
			if err != nil {
				return nil, err
			}
			for i := range list {
				assets = append(assets, &list[i])
			}
		}
	} else {
		for _, id := range assetIDs {
			a, err := s.assetRepo.GetByID(ctx, userID, id)
			if err != nil {
				continue
			}
			assets = append(assets, a)
		}
	}
	if len(assets) == 0 {
		return []RefreshResult{}, nil
	}
	// 构建 assetID → Asset 映射，用于返回 asset_code/name
	assetMap := make(map[uint]*domain.Asset, len(assets))
	for _, a := range assets {
		assetMap[a.ID] = a
	}
	// 转 AssetKey
	keys := make([]platformapi.AssetKey, 0, len(assets))
	for _, a := range assets {
		k := platformapi.AssetKey{
			AssetID:   a.ID,
			AssetType: string(a.AssetType),
			AssetCode: a.AssetCode,
		}
		if a.StockDetail != nil {
			k.Market = a.StockDetail.Market
		}
		keys = append(keys, k)
	}

	src := source
	if src == "auto" {
		src = ""
	}
	results := s.aggregator.FetchBatch(ctx, keys, src)

	out := make([]RefreshResult, 0, len(results))
	for _, r := range results {
		rr := RefreshResult{AssetID: r.AssetID}
		if a, ok := assetMap[r.AssetID]; ok {
			rr.AssetCode = a.AssetCode
			rr.Name = a.Name
		}
		if r.Err != nil {
			rr.OK = false
			rr.Message = r.Err.Error()
			out = append(out, rr)
			slog.Warn("quote refresh failed", "asset_id", r.AssetID, "err", r.Err.Error())
			continue
		}
		// 写入 DB
		pq := &domain.PriceQuote{
			AssetID:   r.AssetID,
			Price:     r.Price,
			ChangePct: r.ChangePct,
			Volume:    r.Volume,
			QuoteTime: r.QuoteTime,
			Source:    r.Source,
			CreatedAt: time.Now(),
		}
		if err := s.repo.Insert(ctx, pq); err != nil {
			rr.OK = false
			rr.Message = err.Error()
			out = append(out, rr)
			continue
		}
		// 失效缓存
		_ = s.cache.Delete(ctx, cacheKeyQuote(r.AssetID))
		rr.OK = true
		rr.Source = r.Source
		rr.Price = r.Price.String()
		out = append(out, rr)
	}
	return out, nil
}

// SaveManual 手动写入一条行情（source=manual）。
func (s *QuoteService) SaveManual(ctx context.Context, q *domain.PriceQuote) error {
	if q == nil {
		return errs.ErrInvalidParam.WithMsg("price quote required")
	}
	if q.AssetID == 0 {
		return errs.ErrInvalidParam.WithMsg("asset_id required")
	}
	if q.Price.LessThanOrEqual(decimal.Zero) {
		return errs.ErrInvalidParam.WithMsg("price must be positive")
	}
	if q.QuoteTime.IsZero() {
		q.QuoteTime = time.Now()
	}
	if q.Source == "" {
		q.Source = domain.QuoteSourceManual
	}
	if err := s.repo.Insert(ctx, q); err != nil {
		return err
	}
	_ = s.cache.Delete(ctx, cacheKeyQuote(q.AssetID))
	return nil
}

// =====================================================================
// helpers
// =====================================================================

func cacheKeyQuote(assetID uint) string { return fmt.Sprintf("quote:latest:%d", assetID) }

func toLatest(q *domain.PriceQuote) LatestQuote {
	return LatestQuote{
		AssetID:   q.AssetID,
		Price:     q.Price.String(),
		ChangePct: q.ChangePct.String(),
		Volume:    q.Volume.String(),
		QuoteTime: q.QuoteTime.Format(time.RFC3339),
		Source:    q.Source,
	}
}
