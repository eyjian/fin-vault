package tools

import (
	"context"
	"fmt"

	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// MarketDataDeps 行情查询工具依赖。
type MarketDataDeps struct {
	Quote repository.QuoteRepository
	Asset repository.AssetRepository
}

// MarketDataArgs LLM 传入参数。
//
// 设计要点（D13 规则 1）：market_data 涉用户数据（按 user_id 查 asset 详情拼 code/name），
// 入参 schema **不**含 user_id 字段——身份从 ctx 提取。
//
// 注：market_data 与 search_fund 的差别：
//   - search_fund 是公共数据查询（按 keyword 模糊匹配全库基金），不需要 D13 隔离
//   - market_data 在拿到行情后会调 AssetRepository.GetByID(ctx, userID, id) 拼 code/name；
//     该接口对 userID=0 的行为是"返回 nil"——这意味着如果不强制 ctx 注入，结果会丢失 asset 元信息
//     （旧版 user_id==0 默认 1 兜底掩盖了这个语义；本次改造遵循 D13 严格 ctx 注入）
type MarketDataArgs struct {
	AssetIDs []uint `json:"asset_ids" jsonschema:"description=要查询的资产 ID 列表（必填，至少一个）,required"`
}

// MarketDataOutput LLM 可见的返回。
type MarketDataOutput struct {
	Items []MarketDataItem `json:"items" jsonschema:"description=行情快照列表"`
	Count int              `json:"count" jsonschema:"description=条数"`
}

// MarketDataItem 单条行情快照。decimal 字段用 string 序列化（铁律 F1）。
type MarketDataItem struct {
	AssetID   uint   `json:"asset_id"`
	AssetCode string `json:"asset_code,omitempty"`
	AssetName string `json:"asset_name,omitempty"`
	Price     string `json:"price"`
	ChangePct string `json:"change_pct,omitempty"`
	QuoteTime string `json:"quote_time,omitempty"`
	Source    string `json:"source,omitempty"`
}

// NewMarketDataTool 构造 market_data 工具：批量按 asset_ids 取最新行情。
//
// D13 强制约束：身份从 ctx 提取（用于拼 asset code/name 时的归属校验），
// 提取失败立即返错，禁止任何兜底。
func NewMarketDataTool(deps MarketDataDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, args MarketDataArgs) (MarketDataOutput, error) {
			uid, ok := UserIDFromContext(ctx)
			if !ok {
				return MarketDataOutput{}, fmt.Errorf("user_id not in context: %w", errs.ErrAIToolCallFailed)
			}
			if len(args.AssetIDs) == 0 {
				return MarketDataOutput{Items: []MarketDataItem{}, Count: 0}, nil
			}
			quotes, err := deps.Quote.BatchGetLatest(ctx, args.AssetIDs)
			if err != nil {
				return MarketDataOutput{}, fmt.Errorf("get latest batch failed: %w", err)
			}
			items := make([]MarketDataItem, 0, len(args.AssetIDs))
			for _, id := range args.AssetIDs {
				q, qok := quotes[id]
				if !qok || q == nil {
					continue
				}
				item := MarketDataItem{
					AssetID:   id,
					Price:     q.Price.String(),
					ChangePct: q.ChangePct.String(),
					QuoteTime: q.QuoteTime.Format("2006-01-02 15:04:05"),
					Source:    q.Source,
				}
				// 拼 asset 元信息：必须使用 ctx 注入的 uid，而非 0 兜底（D13 + 防越权）
				if asset, _ := deps.Asset.GetByID(ctx, uid, id); asset != nil {
					item.AssetCode = asset.AssetCode
					item.AssetName = asset.Name
				}
				items = append(items, item)
			}
			return MarketDataOutput{Items: items, Count: len(items)}, nil
		},
		sdkfunction.WithName("market_data"),
		sdkfunction.WithDescription("批量查询当前用户名下资产的最新价格/净值快照（PriceQuote）。需要传入 asset_ids 数组（至少一个）。返回 items[]{asset_id,asset_code,asset_name,price,change_pct,quote_time,source}。用户身份从 ctx 自动注入，不接受 user_id 参数。"),
	)
}
