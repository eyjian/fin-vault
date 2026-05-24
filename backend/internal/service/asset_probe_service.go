package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/platformapi"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// AssetProbeService —— 资产录入"按代码自动填充"
// =====================================================================
//
// 与 QuoteService 的差别：
//   - QuoteService 关注价格刷新，依赖 QuoteAggregator（多源 + 协程池）；
//   - AssetProbeService 关注用户录入资产时一次性回填可公开获取的元信息，
//     支持多源降级（默认主源东方财富，备用源新浪），低频调用。
//
// service 不直接返回 *platformapi.AssetMeta，而是封装为 ProbeResult，
// 把 decimal/time 等内部类型按"零值省略"原则转成 string，方便 handler 直接写 JSON。

// AssetProbeService 资产元信息探测服务。
type AssetProbeService struct {
	fetchers []platformapi.AssetMetaFetcher // 按优先级排列，第一个为主源，后续为备用源
}

// NewAssetProbeService 构造服务。
//
// fetchers 按优先级顺序传入，主源在前、备用源在后；
// 探测时按顺序尝试，第一个成功即返回；全部失败则返回最后一个错误。
// 传空或全部 nil 时，所有 Probe 请求会返回 ErrAssetProbeUpstream，
// 与 QuoteAggregator 在 LLM/行情未配置时的"降级失败"语义一致。
func NewAssetProbeService(fetchers ...platformapi.AssetMetaFetcher) *AssetProbeService {
	valid := make([]platformapi.AssetMetaFetcher, 0, len(fetchers))
	for _, f := range fetchers {
		if f != nil {
			valid = append(valid, f)
		}
	}
	return &AssetProbeService{fetchers: valid}
}

// ProbeArgs 入参。
type ProbeArgs struct {
	AssetType string // fund / stock（必填）
	AssetCode string // 必填
	Market    string // stock 必填，fund 忽略
}

// ProbeResult 出参。decimal/time 按 string 序列化（与 quote_service 风格一致）。
//
// 所有字段均 omitempty——零值不下发，方便前端"仅填空"逻辑直接判定。
type ProbeResult struct {
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Company     string `json:"company,omitempty"`
	Manager     string `json:"manager,omitempty"`
	FundType    string `json:"fund_type,omitempty"`
	LatestNAV   string `json:"latest_nav,omitempty"`
	NAVDate     string `json:"nav_date,omitempty"`
	Market      string `json:"market,omitempty"`
	Industry    string `json:"industry,omitempty"`
	Sector      string `json:"sector,omitempty"`
	ListingDate string `json:"listing_date,omitempty"`
	LatestPrice string `json:"latest_price,omitempty"`
}

// Probe 按 args 探测资产元信息。
//
// 错误归一化：
//   - 入参非法 → errs.ErrInvalidParam
//   - fetcher 未配置 / 远端 HTTP 错 → errs.ErrAssetProbeUpstream
//   - 远端无数据（platformapi.ErrNoData）→ errs.ErrAssetProbeNotFound
//   - 不支持的资产类型（platformapi.ErrUnsupportedAsset）→ errs.ErrInvalidParam
func (s *AssetProbeService) Probe(ctx context.Context, args ProbeArgs) (*ProbeResult, error) {
	args.AssetType = strings.ToLower(strings.TrimSpace(args.AssetType))
	args.AssetCode = strings.TrimSpace(args.AssetCode)
	args.Market = strings.ToUpper(strings.TrimSpace(args.Market))

	if args.AssetType != "fund" && args.AssetType != "stock" {
		return nil, errs.ErrInvalidParam.WithMsg("asset_type must be one of: fund, stock")
	}
	if args.AssetCode == "" {
		return nil, errs.ErrInvalidParam.WithMsg("asset_code required")
	}
	if len(args.AssetCode) > 32 {
		return nil, errs.ErrInvalidParam.WithMsg("asset_code too long")
	}
	if args.AssetType == "stock" && args.Market == "" {
		return nil, errs.ErrInvalidParam.WithMsg("market required for stock")
	}

	if len(s.fetchers) == 0 {
		return nil, errs.ErrAssetProbeUpstream.WithMsg("asset probe fetcher not configured")
	}

	// 用 5s 超时兜底；如果 ctx 已带 deadline 则直接复用，避免双层超时。
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	// 按优先级逐源尝试，第一个成功即返回；
	// 记录每个源的失败原因，全部失败时返回最后一个非 ErrNoData 的错误（优先返回网络错误），
	// 若全部是 ErrNoData 则返回 ErrAssetProbeNotFound。
	var lastErr error
	allNoData := true
	ak := platformapi.AssetKey{
		AssetType: args.AssetType,
		AssetCode: args.AssetCode,
		Market:    args.Market,
	}
	for _, fetcher := range s.fetchers {
		if !fetcher.Supports(ak) {
			continue
		}
		meta, err := fetcher.FetchMeta(ctx, ak)
		if err == nil {
			if meta == nil {
				continue
			}
			return toProbeResult(meta), nil
		}
		if !errors.Is(err, platformapi.ErrNoData) {
			allNoData = false
		}
		lastErr = err
	}

	// 全部源都失败了，归一化错误
	if lastErr == nil {
		return nil, errs.ErrAssetProbeNotFound
	}
	switch {
	case errors.Is(lastErr, platformapi.ErrNoData) && allNoData:
		return nil, errs.ErrAssetProbeNotFound.WithCause(lastErr)
	case errors.Is(lastErr, platformapi.ErrUnsupportedAsset):
		return nil, errs.ErrInvalidParam.WithCause(lastErr)
	default:
		return nil, errs.ErrAssetProbeUpstream.WithCause(lastErr)
	}
}

// toProbeResult 把 platformapi.AssetMeta 转换为对外的 ProbeResult。
func toProbeResult(m *platformapi.AssetMeta) *ProbeResult {
	r := &ProbeResult{
		Name:     m.Name,
		Source:   m.Source,
		Company:  m.Company,
		Manager:  m.Manager,
		FundType: m.FundType,
		Market:   m.Market,
		Industry: m.Industry,
		Sector:   m.Sector,
	}
	if !m.LatestNAV.IsZero() {
		r.LatestNAV = m.LatestNAV.String()
	}
	if !m.NAVDate.IsZero() {
		r.NAVDate = m.NAVDate.Format("2006-01-02")
	}
	if !m.LatestPrice.IsZero() {
		r.LatestPrice = m.LatestPrice.String()
	}
	if !m.ListingDate.IsZero() {
		r.ListingDate = m.ListingDate.Format("2006-01-02")
	}
	return r
}
