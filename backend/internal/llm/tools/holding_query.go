package tools

import (
	"context"
	"fmt"

	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// HoldingQueryDeps 持仓查询工具依赖。
type HoldingQueryDeps struct {
	Holding repository.HoldingRepository
	Asset   repository.AssetRepository
}

// HoldingQueryArgs LLM 传入参数。
//
// 设计要点（D13 规则 1）：holding_query 涉用户数据，入参 schema **不**含 user_id
// 字段——身份从 ctx 提取，禁止 prompt 越权。
//
// **§6.3 安全修复**：旧版本暴露 user_id 字段且在 user_id==0 时默认 1 兜底，
// 形成两条越权路径（① LLM 传别人的 user_id；② 调用方忘记注入身份时被路由到 user 1
// 的数据）。本次改造：
//
//   1. 删除 args 中的 UserID 字段（schema 物理上不暴露 user_id）
//   2. fn 强制 UserIDFromContext(ctx)，提取失败立即返错（不兜底）
type HoldingQueryArgs struct {
	AssetType  string `json:"asset_type,omitempty"  jsonschema:"description=资产类型过滤: fund/stock/wealth/cash"`
	PlatformID uint   `json:"platform_id,omitempty" jsonschema:"description=平台 ID 过滤"`
	Status     string `json:"status,omitempty"      jsonschema:"description=持仓状态: holding/closed/matured"`
}

// HoldingQueryOutput LLM 可见的返回。
type HoldingQueryOutput struct {
	Items []HoldingItem `json:"items" jsonschema:"description=当前用户持仓列表"`
	Count int           `json:"count" jsonschema:"description=条数"`
}

// HoldingItem 给 LLM 看的精简持仓视图。decimal 字段用 string 序列化（铁律 F1）。
type HoldingItem struct {
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

// holdingQueryPageSize 单次最多返回的持仓条数（覆盖典型零售用户全量持仓）。
const holdingQueryPageSize = 200

// NewHoldingQueryTool 构造 holding_query 工具。
//
// D13 强制约束：
//   - 入参不含 user_id（args 仅有业务过滤维度：asset_type / platform_id / status）
//   - 身份必须从 ctx 提取，提取失败直接返错，禁止任何兜底（user_id==0 不允许）
//   - 调 deps.Holding.ListByUser(ctx, opts) 时 opts.UserID 必填来自 ctx
func NewHoldingQueryTool(deps HoldingQueryDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, args HoldingQueryArgs) (HoldingQueryOutput, error) {
			uid, ok := UserIDFromContext(ctx)
			if !ok {
				return HoldingQueryOutput{}, fmt.Errorf("user_id not in context: %w", errs.ErrAIToolCallFailed)
			}
			filters := map[string]any{}
			if args.AssetType != "" {
				filters["asset_type"] = args.AssetType
			}
			if args.PlatformID != 0 {
				filters["platform_id"] = args.PlatformID
			}
			if args.Status != "" {
				filters["status"] = args.Status
			}
			holdings, _, err := deps.Holding.ListByUser(ctx, repository.ListOptions{
				UserID:   uid,
				Page:     1,
				PageSize: holdingQueryPageSize,
				Filters:  filters,
			})
			if err != nil {
				return HoldingQueryOutput{}, fmt.Errorf("list holdings failed: %w", err)
			}
			items := make([]HoldingItem, 0, len(holdings))
			for i := range holdings {
				h := &holdings[i]
				it := HoldingItem{
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
				} else if asset, _ := deps.Asset.GetByID(ctx, uid, h.AssetID); asset != nil {
					it.AssetCode = asset.AssetCode
					it.AssetName = asset.Name
					it.AssetType = string(asset.AssetType)
					it.Currency = asset.Currency
				}
				items = append(items, it)
			}
			return HoldingQueryOutput{Items: items, Count: len(items)}, nil
		},
		sdkfunction.WithName("holding_query"),
		sdkfunction.WithDescription("查询当前用户的持仓明细。可按 asset_type(fund/stock/wealth/cash)、platform_id、status 过滤。用户身份从 ctx 自动注入，不接受 user_id 参数。"),
	)
}
