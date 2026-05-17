package tools

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// PlatformSummaryDeps 平台资产汇总工具依赖。
type PlatformSummaryDeps struct {
	Holding  repository.HoldingRepository
	Platform repository.PlatformRepository
	Quote    repository.QuoteRepository
}

// PlatformSummaryArgs LLM 传入参数。
//
// 设计要点（D13 规则 1）：platform_summary 涉用户数据，入参 schema **不**含 user_id。
// 删除旧版 user_id 字段后，本工具不再需要任何业务过滤入参；JSON Schema 渲染为
// `{"type":"object"}`（无 properties，是合法 schema）——这是合规且符合预期的。
type PlatformSummaryArgs struct{}

// PlatformSummaryOutput 平台维度的资产汇总。
type PlatformSummaryOutput struct {
	Items []PlatformAggregate `json:"items" jsonschema:"description=按平台聚合的持仓快照"`
	Count int                 `json:"count" jsonschema:"description=平台数量"`
}

// PlatformAggregate 单个平台的聚合行。decimal 字段用 string 序列化（铁律 F1）。
type PlatformAggregate struct {
	PlatformID   uint   `json:"platform_id"`
	PlatformName string `json:"platform_name"`
	HoldingCount int    `json:"holding_count"`
	TotalCost    string `json:"total_cost"`
	MarketValue  string `json:"market_value"`
	TotalPnL     string `json:"total_pnl"`
}

// platformSummaryPageSize 单次拉取的持仓上限（覆盖典型零售用户全量持仓）。
const platformSummaryPageSize = 500

// NewPlatformSummaryTool 构造 platform_summary 工具：按平台聚合当前用户持仓。
//
// D13 强制约束：身份从 ctx 提取，入参 schema 物理上不含 user_id，禁止任何兜底。
func NewPlatformSummaryTool(deps PlatformSummaryDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, _ PlatformSummaryArgs) (PlatformSummaryOutput, error) {
			uid, ok := UserIDFromContext(ctx)
			if !ok {
				return PlatformSummaryOutput{}, fmt.Errorf("user_id not in context: %w", errs.ErrAIToolCallFailed)
			}
			holdings, _, err := deps.Holding.ListByUser(ctx, repository.ListOptions{
				UserID:   uid,
				Page:     1,
				PageSize: platformSummaryPageSize,
				Filters:  map[string]any{"status": "holding"},
			})
			if err != nil {
				return PlatformSummaryOutput{}, fmt.Errorf("list holdings failed: %w", err)
			}
			ids := make([]uint, 0, len(holdings))
			for i := range holdings {
				ids = append(ids, holdings[i].AssetID)
			}
			quoteMap, _ := deps.Quote.BatchGetLatest(ctx, ids)

			platforms, _ := deps.Platform.List(ctx)
			platformName := make(map[uint]string, len(platforms))
			for i := range platforms {
				platformName[platforms[i].ID] = platforms[i].Name
			}

			agg := make(map[uint]*PlatformAggregate)
			costMap := make(map[uint]decimal.Decimal)
			marketMap := make(map[uint]decimal.Decimal)
			pnlMap := make(map[uint]decimal.Decimal)
			for i := range holdings {
				h := &holdings[i]
				row, exist := agg[h.PlatformID]
				if !exist {
					row = &PlatformAggregate{PlatformID: h.PlatformID, PlatformName: platformName[h.PlatformID]}
					agg[h.PlatformID] = row
					costMap[h.PlatformID] = decimal.Zero
					marketMap[h.PlatformID] = decimal.Zero
					pnlMap[h.PlatformID] = decimal.Zero
				}
				price := decimal.Zero
				if q, qok := quoteMap[h.AssetID]; qok && q != nil {
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
			items := make([]PlatformAggregate, 0, len(agg))
			for pid, row := range agg {
				row.TotalCost = costMap[pid].String()
				row.MarketValue = marketMap[pid].String()
				row.TotalPnL = pnlMap[pid].String()
				items = append(items, *row)
			}
			return PlatformSummaryOutput{Items: items, Count: len(items)}, nil
		},
		sdkfunction.WithName("platform_summary"),
		sdkfunction.WithDescription("按平台聚合当前用户的持仓总成本/总市值/总盈亏，供资产配置建议参考。无业务过滤参数。用户身份从 ctx 自动注入，不接受 user_id 参数。"),
	)
}
