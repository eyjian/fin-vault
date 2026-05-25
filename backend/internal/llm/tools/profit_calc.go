package tools

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// ProfitCalcDeps 盈亏计算工具依赖。
type ProfitCalcDeps struct {
	Holding repository.HoldingRepository
	Quote   repository.QuoteRepository
}

// ProfitCalcArgs LLM 传入参数。
//
// 设计要点（D13 规则 1）：profit_calc 涉用户数据，入参 schema **不**含 user_id。
type ProfitCalcArgs struct {
	AssetType string `json:"asset_type,omitempty" jsonschema:"description=资产类型过滤: fund/stock/wealth/cash"`
}

// ProfitCalcOutput 盈亏汇总结果。decimal 全部 string 序列化（铁律 F1）。
type ProfitCalcOutput struct {
	TotalCost     string                `json:"total_cost"      jsonschema:"description=持仓总成本"`
	TotalMarket   string                `json:"total_market_value" jsonschema:"description=持仓总市值"`
	TotalRealized string                `json:"total_realized_pnl" jsonschema:"description=已实现盈亏总和"`
	TotalDividend string                `json:"total_dividend"     jsonschema:"description=分红总和"`
	UnrealizedPnL string                `json:"unrealized_pnl"     jsonschema:"description=未实现盈亏（市值-成本）"`
	TotalPnL      string                `json:"total_pnl"          jsonschema:"description=总盈亏 = 未实现 + 已实现 + 分红"`
	PnLRatio      string                `json:"pnl_ratio"          jsonschema:"description=收益率 = 总盈亏 / 总成本"`
	ByHolding     []ProfitCalcHoldingItem `json:"by_holding,omitempty" jsonschema:"description=按持仓的明细"`
}

// ProfitCalcHoldingItem 单个持仓的盈亏快照。
type ProfitCalcHoldingItem struct {
	HoldingID   uint   `json:"holding_id"`
	AssetID     uint   `json:"asset_id"`
	Quantity    string `json:"quantity"`
	AvgCost     string `json:"avg_cost"`
	LatestPrice string `json:"latest_price"`
	MarketValue string `json:"market_value"`
	UnrealPnL   string `json:"unrealized_pnl"`
	TotalPnL    string `json:"total_pnl"`
}

// profitCalcPageSize 一次拉取的持仓上限（覆盖典型零售用户全量持仓）。
const profitCalcPageSize = 500

// NewProfitCalcTool 构造 profit_calc 工具：基于持仓 + 最新行情计算总盈亏。
//
// D13 强制约束：身份从 ctx 提取，禁止入参 schema 暴露 user_id，禁止任何兜底。
func NewProfitCalcTool(deps ProfitCalcDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, args ProfitCalcArgs) (ProfitCalcOutput, error) {
			uid, ok := UserIDFromContext(ctx)
			if !ok {
				return ProfitCalcOutput{}, fmt.Errorf("user_id not in context: %w", errs.ErrAIToolCallFailed)
			}
			filters := map[string]any{"status": domain.HoldingStatusHolding}
			if args.AssetType != "" {
				filters["asset_type"] = args.AssetType
			}
			holdings, _, err := deps.Holding.ListByUser(ctx, repository.ListOptions{
				UserID:   uid,
				Page:     1,
				PageSize: profitCalcPageSize,
				Filters:  filters,
			})
			if err != nil {
				return ProfitCalcOutput{}, fmt.Errorf("list holdings failed: %w", err)
			}
			ids := make([]uint, 0, len(holdings))
			for i := range holdings {
				ids = append(ids, holdings[i].AssetID)
			}
			quoteMap, _ := deps.Quote.BatchGetLatest(ctx, ids)

			totalCost := decimal.Zero
			totalMarket := decimal.Zero
			totalRealized := decimal.Zero
			totalDividend := decimal.Zero
			items := make([]ProfitCalcHoldingItem, 0, len(holdings))
			for i := range holdings {
				h := &holdings[i]
				price := decimal.Zero
				if q, qok := quoteMap[h.AssetID]; qok && q != nil {
					price = q.Price
				}
				marketVal := h.Quantity.Mul(price)
				unrealPnL := marketVal.Sub(h.TotalCost)
				totalPnL := unrealPnL.Add(h.RealizedPnL).Add(h.TotalDividend)
				totalCost = totalCost.Add(h.TotalCost)
				totalMarket = totalMarket.Add(marketVal)
				totalRealized = totalRealized.Add(h.RealizedPnL)
				totalDividend = totalDividend.Add(h.TotalDividend)
				items = append(items, ProfitCalcHoldingItem{
					HoldingID:   h.ID,
					AssetID:     h.AssetID,
					Quantity:    h.Quantity.String(),
					AvgCost:     h.AvgCost.String(),
					LatestPrice: price.String(),
					MarketValue: marketVal.String(),
					UnrealPnL:   unrealPnL.String(),
					TotalPnL:    totalPnL.String(),
				})
			}
			unrealAll := totalMarket.Sub(totalCost)
			pnlAll := unrealAll.Add(totalRealized).Add(totalDividend)
			ratio := decimal.Zero
			if !totalCost.IsZero() {
				ratio = pnlAll.Div(totalCost).Round(6)
			}
			return ProfitCalcOutput{
				TotalCost:     totalCost.String(),
				TotalMarket:   totalMarket.String(),
				TotalRealized: totalRealized.String(),
				TotalDividend: totalDividend.String(),
				UnrealizedPnL: unrealAll.String(),
				TotalPnL:      pnlAll.String(),
				PnLRatio:      ratio.String(),
				ByHolding:     items,
			}, nil
		},
		sdkfunction.WithName("profit_calc"),
		sdkfunction.WithDescription("计算当前用户的总盈亏：持仓总成本 / 总市值 / 已实现 / 未实现 / 总盈亏 / 收益率，并附按持仓的明细。可按 asset_type 过滤。用户身份从 ctx 自动注入，不接受 user_id 参数。"),
	)
}
