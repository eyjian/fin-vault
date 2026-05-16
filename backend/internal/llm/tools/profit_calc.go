package tools

import (
	"context"
	"encoding/json"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// ProfitCalcDeps 盈亏计算工具依赖。
type ProfitCalcDeps struct {
	Holding repository.HoldingRepository
	Quote   repository.QuoteRepository
}

type profitCalcArgs struct {
	UserID    uint   `json:"user_id"`
	AssetType string `json:"asset_type,omitempty"`
}

type profitCalcOutput struct {
	TotalCost     string                  `json:"total_cost"`
	TotalMarket   string                  `json:"total_market_value"`
	TotalRealized string                  `json:"total_realized_pnl"`
	TotalDividend string                  `json:"total_dividend"`
	UnrealizedPnL string                  `json:"unrealized_pnl"`
	TotalPnL      string                  `json:"total_pnl"`
	PnLRatio      string                  `json:"pnl_ratio"`
	ByHolding     []profitCalcHoldingItem `json:"by_holding,omitempty"`
}

type profitCalcHoldingItem struct {
	HoldingID   uint   `json:"holding_id"`
	AssetID     uint   `json:"asset_id"`
	Quantity    string `json:"quantity"`
	AvgCost     string `json:"avg_cost"`
	LatestPrice string `json:"latest_price"`
	MarketValue string `json:"market_value"`
	UnrealPnL   string `json:"unrealized_pnl"`
	TotalPnL    string `json:"total_pnl"`
}

// NewProfitCalcTool 盈亏计算工具：基于持仓 + 最新行情，计算总成本/市值/未实现/已实现/总盈亏。
func NewProfitCalcTool(deps ProfitCalcDeps) llm.Tool {
	return llm.Tool{
		Name:        "profit_calc",
		Description: "计算用户当前总盈亏（持仓总成本/总市值/已实现/未实现/总盈亏/收益率）。可按 asset_type 过滤。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id":    map[string]any{"type": "integer", "description": "默认 1"},
				"asset_type": map[string]any{"type": "string", "enum": []string{"fund", "stock", "wealth", "cash"}},
			},
		},
		Handler: func(ctx context.Context, raw string) (string, error) {
			var a profitCalcArgs
			if err := llm.SafeUnmarshalArgs(raw, &a); err != nil {
				return errString("invalid args", err), nil
			}
			if a.UserID == 0 {
				a.UserID = 1
			}
			filters := map[string]any{"status": "holding"}
			if a.AssetType != "" {
				filters["asset_type"] = a.AssetType
			}
			holdings, _, err := deps.Holding.ListByUser(ctx, repository.ListOptions{
				UserID:   a.UserID,
				Page:     1,
				PageSize: 500,
				Filters:  filters,
			})
			if err != nil {
				return errString("list holdings failed", err), nil
			}
			ids := make([]uint, 0, len(holdings))
			for _, h := range holdings {
				ids = append(ids, h.AssetID)
			}
			quoteMap, _ := deps.Quote.BatchGetLatest(ctx, ids)

			totalCost := decimal.Zero
			totalMarket := decimal.Zero
			totalRealized := decimal.Zero
			totalDividend := decimal.Zero
			items := make([]profitCalcHoldingItem, 0, len(holdings))
			for _, h := range holdings {
				price := decimal.Zero
				if q, ok := quoteMap[h.AssetID]; ok && q != nil {
					price = q.Price
				}
				marketVal := h.Quantity.Mul(price)
				unrealPnL := marketVal.Sub(h.TotalCost)
				totalPnL := unrealPnL.Add(h.RealizedPnL).Add(h.TotalDividend)
				totalCost = totalCost.Add(h.TotalCost)
				totalMarket = totalMarket.Add(marketVal)
				totalRealized = totalRealized.Add(h.RealizedPnL)
				totalDividend = totalDividend.Add(h.TotalDividend)
				items = append(items, profitCalcHoldingItem{
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
			out := profitCalcOutput{
				TotalCost:     totalCost.String(),
				TotalMarket:   totalMarket.String(),
				TotalRealized: totalRealized.String(),
				TotalDividend: totalDividend.String(),
				UnrealizedPnL: unrealAll.String(),
				TotalPnL:      pnlAll.String(),
				PnLRatio:      ratio.String(),
				ByHolding:     items,
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		},
	}
}
