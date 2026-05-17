package tools

import (
	"context"
	"fmt"
	"time"

	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// HistoryQueryDeps 历史交易查询工具依赖。
type HistoryQueryDeps struct {
	Transaction repository.TransactionRepository
}

// HistoryQueryArgs LLM 传入参数。
//
// 设计要点（D13 规则 1）：history_query 涉用户数据（查交易流水），入参 schema
// **不**含 user_id 字段——身份从 ctx 提取，prompt 物理上无法越权。
type HistoryQueryArgs struct {
	HoldingID  uint   `json:"holding_id,omitempty"  jsonschema:"description=持仓 ID 过滤"`
	AssetID    uint   `json:"asset_id,omitempty"    jsonschema:"description=资产 ID 过滤"`
	PlatformID uint   `json:"platform_id,omitempty" jsonschema:"description=平台 ID 过滤"`
	TxnType    string `json:"txn_type,omitempty"    jsonschema:"description=交易类型: buy/sell/dividend/dividend_reinvest/split/bonus/mature/interest/deposit/withdraw/cash_in/cash_out/adjust"`
	Start      string `json:"start,omitempty"       jsonschema:"description=开始日期 YYYY-MM-DD"`
	End        string `json:"end,omitempty"         jsonschema:"description=结束日期 YYYY-MM-DD"`
	Limit      int    `json:"limit,omitempty"       jsonschema:"description=返回条数上限，默认 100，最大 200"`
}

// HistoryQueryOutput LLM 可见的返回。
type HistoryQueryOutput struct {
	Items []HistoryItem `json:"items" jsonschema:"description=历史交易记录列表"`
	Count int           `json:"count" jsonschema:"description=记录条数"`
	Total int64         `json:"total" jsonschema:"description=匹配总数（含未返回的部分）"`
}

// HistoryItem 单条交易摘要。decimal 字段用 string 序列化，避免精度损失（铁律 F1）。
type HistoryItem struct {
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

const (
	historyDefaultLimit = 100
	historyMaxLimit     = 200
)

// NewHistoryQueryTool 构造 history_query 工具。
//
// D13 强制约束：身份从 ctx 提取（service 层注入），禁止入参 schema 暴露 user_id
// 字段，禁止任何兜底（如旧版 `if a.UserID == 0 { a.UserID = 1 }`）。
func NewHistoryQueryTool(deps HistoryQueryDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, args HistoryQueryArgs) (HistoryQueryOutput, error) {
			uid, ok := UserIDFromContext(ctx)
			if !ok {
				return HistoryQueryOutput{}, fmt.Errorf("user_id not in context: %w", errs.ErrAIToolCallFailed)
			}
			limit := args.Limit
			if limit <= 0 {
				limit = historyDefaultLimit
			}
			if limit > historyMaxLimit {
				limit = historyMaxLimit
			}
			filters := map[string]any{}
			if args.HoldingID != 0 {
				filters["holding_id"] = args.HoldingID
			}
			if args.AssetID != 0 {
				filters["asset_id"] = args.AssetID
			}
			if args.PlatformID != 0 {
				filters["platform_id"] = args.PlatformID
			}
			if args.TxnType != "" {
				filters["txn_type"] = args.TxnType
			}
			if args.Start != "" {
				if t, err := time.Parse("2006-01-02", args.Start); err == nil {
					filters["start_time"] = t
				}
			}
			if args.End != "" {
				if t, err := time.Parse("2006-01-02", args.End); err == nil {
					// 截止日含当天 23:59:59，统一加 1 天作为开区间右端
					filters["end_time"] = t.Add(24 * time.Hour)
				}
			}
			txns, total, err := deps.Transaction.List(ctx, repository.ListOptions{
				UserID:   uid,
				Page:     1,
				PageSize: limit,
				OrderBy:  "f_txn_time desc",
				Filters:  filters,
			})
			if err != nil {
				return HistoryQueryOutput{}, fmt.Errorf("list transactions failed: %w", err)
			}
			items := make([]HistoryItem, 0, len(txns))
			for i := range txns {
				t := &txns[i]
				items = append(items, HistoryItem{
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
			return HistoryQueryOutput{Items: items, Count: len(items), Total: total}, nil
		},
		sdkfunction.WithName("history_query"),
		sdkfunction.WithDescription("查询当前用户的历史交易流水。可按 holding_id / asset_id / platform_id / txn_type / 时间范围过滤。返回最多 limit 条（默认 100，最大 200）。用户身份从 ctx 自动注入，不接受 user_id 参数。"),
	)
}
