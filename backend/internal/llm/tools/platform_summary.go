package tools

import (
	"context"
	"encoding/json"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// PlatformSummaryDeps 平台资产汇总工具依赖。
type PlatformSummaryDeps struct {
	Holding  repository.HoldingRepository
	Platform repository.PlatformRepository
	Quote    repository.QuoteRepository
}

type platformSummaryArgs struct {
	UserID uint `json:"user_id"`
}

type platformAggregate struct {
	PlatformID   uint   `json:"platform_id"`
	PlatformName string `json:"platform_name"`
	HoldingCount int    `json:"holding_count"`
	TotalCost    string `json:"total_cost"`
	MarketValue  string `json:"market_value"`
	TotalPnL     string `json:"total_pnl"`
}

// NewPlatformSummaryTool 平台资产汇总：按 platform_id 聚合持仓、市值与盈亏。
func NewPlatformSummaryTool(deps PlatformSummaryDeps) llm.Tool {
	return llm.Tool{
		Name:        "platform_summary",
		Description: "按平台聚合用户当前持仓的总成本/总市值/总盈亏。供资产配置建议参考。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id": map[string]any{"type": "integer", "description": "默认 1"},
			},
		},
		Handler: func(ctx context.Context, raw string) (string, error) {
			var a platformSummaryArgs
			if err := llm.SafeUnmarshalArgs(raw, &a); err != nil {
				return errString("invalid args", err), nil
			}
			if a.UserID == 0 {
				a.UserID = 1
			}
			holdings, _, err := deps.Holding.ListByUser(ctx, repository.ListOptions{
				UserID:   a.UserID,
				Page:     1,
				PageSize: 500,
				Filters:  map[string]any{"status": "holding"},
			})
			if err != nil {
				return errString("list holdings failed", err), nil
			}
			ids := make([]uint, 0, len(holdings))
			for _, h := range holdings {
				ids = append(ids, h.AssetID)
			}
			quoteMap, _ := deps.Quote.BatchGetLatest(ctx, ids)

			platforms, _ := deps.Platform.List(ctx)
			platformName := make(map[uint]string, len(platforms))
			for _, p := range platforms {
				platformName[p.ID] = p.Name
			}

			agg := make(map[uint]*platformAggregate)
			costMap := make(map[uint]decimal.Decimal)
			marketMap := make(map[uint]decimal.Decimal)
			pnlMap := make(map[uint]decimal.Decimal)
			for _, h := range holdings {
				row, ok := agg[h.PlatformID]
				if !ok {
					row = &platformAggregate{PlatformID: h.PlatformID, PlatformName: platformName[h.PlatformID]}
					agg[h.PlatformID] = row
					costMap[h.PlatformID] = decimal.Zero
					marketMap[h.PlatformID] = decimal.Zero
					pnlMap[h.PlatformID] = decimal.Zero
				}
				price := decimal.Zero
				if q, ok := quoteMap[h.AssetID]; ok && q != nil {
					price = q.Price
				}
				marketVal := h.Quantity.Mul(price)
				costMap[h.PlatformID] = costMap[h.PlatformID].Add(h.TotalCost)
				marketMap[h.PlatformID] = marketMap[h.PlatformID].Add(marketVal)
				pnlMap[h.PlatformID] = pnlMap[h.PlatformID].
					Add(marketVal.Sub(h.TotalCost)).
					Add(h.RealizedPnL).
					Add(h.TotalDividend)
				row.HoldingCount++
			}
			out := make([]platformAggregate, 0, len(agg))
			for pid, row := range agg {
				row.TotalCost = costMap[pid].String()
				row.MarketValue = marketMap[pid].String()
				row.TotalPnL = pnlMap[pid].String()
				out = append(out, *row)
			}
			b, _ := json.Marshal(map[string]any{"items": out, "count": len(out)})
			return string(b), nil
		},
	}
}
