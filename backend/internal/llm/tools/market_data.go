package tools

import (
	"context"
	"encoding/json"

	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// MarketDataDeps 行情查询工具依赖。
type MarketDataDeps struct {
	Quote repository.QuoteRepository
	Asset repository.AssetRepository
}

type marketDataArgs struct {
	UserID   uint   `json:"user_id"`
	AssetIDs []uint `json:"asset_ids"`
}

type marketDataItem struct {
	AssetID   uint   `json:"asset_id"`
	AssetCode string `json:"asset_code,omitempty"`
	AssetName string `json:"asset_name,omitempty"`
	Price     string `json:"price"`
	ChangePct string `json:"change_pct,omitempty"`
	QuoteTime string `json:"quote_time,omitempty"`
	Source    string `json:"source,omitempty"`
}

// NewMarketDataTool 行情查询工具：批量按 asset_ids 取最新价。
func NewMarketDataTool(deps MarketDataDeps) llm.Tool {
	return llm.Tool{
		Name:        "market_data",
		Description: "批量查询资产的最新价格/净值快照（PriceQuote）。需要传入 asset_ids 数组。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id": map[string]any{"type": "integer", "description": "默认 1"},
				"asset_ids": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "integer"},
					"description": "要查询的资产 ID 列表",
				},
			},
			"required": []string{"asset_ids"},
		},
		Handler: func(ctx context.Context, raw string) (string, error) {
			var a marketDataArgs
			if err := llm.SafeUnmarshalArgs(raw, &a); err != nil {
				return errString("invalid args", err), nil
			}
			if a.UserID == 0 {
				a.UserID = 1
			}
			if len(a.AssetIDs) == 0 {
				b, _ := json.Marshal(map[string]any{"items": []any{}, "count": 0})
				return string(b), nil
			}
			quotes, err := deps.Quote.BatchGetLatest(ctx, a.AssetIDs)
			if err != nil {
				return errString("get latest batch failed", err), nil
			}
			out := make([]marketDataItem, 0, len(a.AssetIDs))
			for _, id := range a.AssetIDs {
				q, ok := quotes[id]
				if !ok || q == nil {
					continue
				}
				item := marketDataItem{
					AssetID:   id,
					Price:     q.Price.String(),
					ChangePct: q.ChangePct.String(),
					QuoteTime: q.QuoteTime.Format("2006-01-02 15:04:05"),
					Source:    q.Source,
				}
				if asset, _ := deps.Asset.GetByID(ctx, a.UserID, id); asset != nil {
					item.AssetCode = asset.AssetCode
					item.AssetName = asset.Name
				}
				out = append(out, item)
			}
			b, _ := json.Marshal(map[string]any{"items": out, "count": len(out)})
			return string(b), nil
		},
	}
}
