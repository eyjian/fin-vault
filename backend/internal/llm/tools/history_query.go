package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/eyjian/fin-vault/backend/internal/llm"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// HistoryQueryDeps 历史交易查询工具依赖。
type HistoryQueryDeps struct {
	Transaction repository.TransactionRepository
}

type historyQueryArgs struct {
	UserID     uint   `json:"user_id"`
	HoldingID  uint   `json:"holding_id,omitempty"`
	AssetID    uint   `json:"asset_id,omitempty"`
	PlatformID uint   `json:"platform_id,omitempty"`
	TxnType    string `json:"txn_type,omitempty"`
	Start      string `json:"start,omitempty"` // YYYY-MM-DD
	End        string `json:"end,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type historyItem struct {
	ID         uint   `json:"id"`
	HoldingID  uint   `json:"holding_id"`
	AssetID    uint   `json:"asset_id"`
	PlatformID uint   `json:"platform_id"`
	TxnType    string `json:"txn_type"`
	TxnTime    string `json:"txn_time"`
	Quantity   string `json:"quantity"`
	Price      string `json:"price"`
	Amount     string `json:"amount"`
	Fee        string `json:"fee"`
	Tax        string `json:"tax"`
	NetAmount  string `json:"net_amount"`
	Currency   string `json:"currency"`
}

// NewHistoryQueryTool 查询历史交易记录。
func NewHistoryQueryTool(deps HistoryQueryDeps) llm.Tool {
	return llm.Tool{
		Name:        "history_query",
		Description: "查询历史交易流水（Transaction）。可按 holding_id / asset_id / platform_id / txn_type / 时间范围过滤。返回最多 limit 条（默认 100，最大 200）。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id":     map[string]any{"type": "integer", "description": "默认 1"},
				"holding_id":  map[string]any{"type": "integer"},
				"asset_id":    map[string]any{"type": "integer"},
				"platform_id": map[string]any{"type": "integer"},
				"txn_type":    map[string]any{"type": "string", "description": "buy/sell/dividend/dividend_reinvest/split/bonus/mature/interest/deposit/withdraw/cash_in/cash_out/adjust"},
				"start":       map[string]any{"type": "string", "description": "YYYY-MM-DD"},
				"end":         map[string]any{"type": "string", "description": "YYYY-MM-DD"},
				"limit":       map[string]any{"type": "integer", "description": "默认 100，最大 200"},
			},
		},
		Handler: func(ctx context.Context, raw string) (string, error) {
			var a historyQueryArgs
			if err := llm.SafeUnmarshalArgs(raw, &a); err != nil {
				return errString("invalid args", err), nil
			}
			if a.UserID == 0 {
				a.UserID = 1
			}
			limit := a.Limit
			if limit <= 0 {
				limit = 100
			}
			if limit > 200 {
				limit = 200
			}
			filters := map[string]any{}
			if a.HoldingID != 0 {
				filters["holding_id"] = a.HoldingID
			}
			if a.AssetID != 0 {
				filters["asset_id"] = a.AssetID
			}
			if a.PlatformID != 0 {
				filters["platform_id"] = a.PlatformID
			}
			if a.TxnType != "" {
				filters["txn_type"] = a.TxnType
			}
			if a.Start != "" {
				if t, err := time.Parse("2006-01-02", a.Start); err == nil {
					filters["start_time"] = t
				}
			}
			if a.End != "" {
				if t, err := time.Parse("2006-01-02", a.End); err == nil {
					filters["end_time"] = t.Add(24 * time.Hour)
				}
			}
			txns, total, err := deps.Transaction.List(ctx, repository.ListOptions{
				UserID:   a.UserID,
				Page:     1,
				PageSize: limit,
				OrderBy:  "f_txn_time desc",
				Filters:  filters,
			})
			if err != nil {
				return errString("list transactions failed", err), nil
			}
			out := make([]historyItem, 0, len(txns))
			for _, t := range txns {
				out = append(out, historyItem{
					ID:         t.ID,
					HoldingID:  t.HoldingID,
					AssetID:    t.AssetID,
					PlatformID: t.PlatformID,
					TxnType:    string(t.TxnType),
					TxnTime:    t.TxnTime.Format(time.RFC3339),
					Quantity:   t.Quantity.String(),
					Price:      t.Price.String(),
					Amount:     t.Amount.String(),
					Fee:        t.Fee.String(),
					Tax:        t.Tax.String(),
					NetAmount:  t.NetAmount.String(),
					Currency:   t.Currency,
				})
			}
			b, _ := json.Marshal(map[string]any{"items": out, "total": total})
			return string(b), nil
		},
	}
}
