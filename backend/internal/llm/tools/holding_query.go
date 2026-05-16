package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// HoldingQueryDeps 持仓查询工具依赖。
type HoldingQueryDeps struct {
	Holding repository.HoldingRepository
	Asset   repository.AssetRepository
}

// holdingQueryArgs LLM 传入参数。
type holdingQueryArgs struct {
	UserID     uint   `json:"user_id"`
	AssetType  string `json:"asset_type,omitempty"`
	PlatformID uint   `json:"platform_id,omitempty"`
	Status     string `json:"status,omitempty"`
}

// holdingItem 给 LLM 看的精简持仓视图。
type holdingItem struct {
	ID         uint   `json:"id"`
	AssetID    uint   `json:"asset_id"`
	AssetCode  string `json:"asset_code,omitempty"`
	AssetName  string `json:"asset_name,omitempty"`
	AssetType  string `json:"asset_type,omitempty"`
	PlatformID uint   `json:"platform_id"`
	Quantity   string `json:"quantity"`
	AvgCost    string `json:"avg_cost"`
	TotalCost  string `json:"total_cost"`
	Currency   string `json:"currency,omitempty"`
	Status     string `json:"status"`
}

// NewHoldingQueryTool 注册 holding_query 工具，让 LLM 能查询当前用户持仓。
func NewHoldingQueryTool(deps HoldingQueryDeps) llm.Tool {
	return llm.Tool{
		Name:        "holding_query",
		Description: "查询当前用户持仓明细。可按 asset_type(fund/stock/wealth/cash)、platform_id、status 过滤。返回 JSON 数组。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id":     map[string]any{"type": "integer", "description": "用户 ID，默认 1"},
				"asset_type":  map[string]any{"type": "string", "enum": []string{"fund", "stock", "wealth", "cash"}},
				"platform_id": map[string]any{"type": "integer"},
				"status":      map[string]any{"type": "string", "enum": []string{"holding", "closed", "matured"}},
			},
		},
		Handler: func(ctx context.Context, raw string) (string, error) {
			var a holdingQueryArgs
			if err := llm.SafeUnmarshalArgs(raw, &a); err != nil {
				return errString("invalid args", err), nil
			}
			if a.UserID == 0 {
				a.UserID = 1
			}
			filters := map[string]any{}
			if a.AssetType != "" {
				filters["asset_type"] = a.AssetType
			}
			if a.PlatformID != 0 {
				filters["platform_id"] = a.PlatformID
			}
			if a.Status != "" {
				filters["status"] = a.Status
			}
			opts := repository.ListOptions{
				UserID:   a.UserID,
				Page:     1,
				PageSize: 200,
				Filters:  filters,
			}
			holdings, _, err := deps.Holding.ListByUser(ctx, opts)
			if err != nil {
				return errString("list holdings failed", err), nil
			}
			out := make([]holdingItem, 0, len(holdings))
			for _, h := range holdings {
				it := holdingItem{
					ID:         h.ID,
					AssetID:    h.AssetID,
					PlatformID: h.PlatformID,
					Quantity:   h.Quantity.String(),
					AvgCost:    h.AvgCost.String(),
					TotalCost:  h.TotalCost.String(),
					Status:     string(h.Status),
				}
				if h.Asset != nil {
					it.AssetCode = h.Asset.AssetCode
					it.AssetName = h.Asset.Name
					it.AssetType = string(h.Asset.AssetType)
					it.Currency = h.Asset.Currency
				} else if asset, _ := deps.Asset.GetByID(ctx, a.UserID, h.AssetID); asset != nil {
					it.AssetCode = asset.AssetCode
					it.AssetName = asset.Name
					it.AssetType = string(asset.AssetType)
					it.Currency = asset.Currency
				}
				out = append(out, it)
			}
			b, _ := json.Marshal(map[string]any{"items": out, "count": len(out)})
			return string(b), nil
		},
	}
}

func errString(msg string, err error) string {
	b, _ := json.Marshal(map[string]any{
		"error":   true,
		"message": fmt.Sprintf("%s: %v", msg, err),
	})
	return string(b)
}
