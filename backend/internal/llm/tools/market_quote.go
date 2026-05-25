package tools

import (
	"context"
	"fmt"
	"strings"

	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// MarketQuoteDeps market_quote 工具依赖。
type MarketQuoteDeps struct {
	Quote repository.QuoteRepository
	Asset repository.AssetRepository
}

// MarketQuoteArgs LLM 传入参数。
//
// 设计要点（D13 规则 1 + architect 决策）：fin-vault 是个人理财系统，
// asset 表强制 user_id 归属（无"公共资产"概念）；"查上证指数" 实际是
// "查我已记录的指数当前行情"。入参 schema **不暴露 user_id**，从 ctx 拿
// （tools.UserIDFromContext）。
type MarketQuoteArgs struct {
	Symbol string `json:"symbol" jsonschema:"description=资产代码（如\"sh000001\"上证指数 / 基金代码 / 股票代码）,required"`
}

// MarketQuoteOutput LLM 可见的返回。
//
// 字段说明：
//   - Price: 字符串化的 decimal（避免 LLM JSON 解析丢精度）
//   - ChangePercent: 当日涨跌幅（%）；PriceQuote 没有独立的 Change 绝对值字段，
//     仅 ChangePct 可用（按 architect 备注）
//   - UpdatedAt: 报价时间，格式化为 "YYYY-MM-DD HH:MM:SS"
type MarketQuoteOutput struct {
	Symbol        string `json:"symbol" jsonschema:"description=资产代码"`
	Name          string `json:"name" jsonschema:"description=资产名称"`
	Price         string `json:"price" jsonschema:"description=最新价格（字符串化 decimal）"`
	ChangePercent string `json:"change_percent,omitempty" jsonschema:"description=当日涨跌幅（%，字符串化 decimal）"`
	UpdatedAt     string `json:"updated_at" jsonschema:"description=报价时间，格式 YYYY-MM-DD HH:MM:SS"`
}

// marketQuoteAssetTypes asset 多 type 兜底顺序：用户加过的同 symbol 资产可能是
// stock / fund / wealth 任一类型，按"行情资产更常见"的优先级依次尝试。
//
// AssetTypeCash 不参与（现金资产不需要查行情）。
var marketQuoteAssetTypes = []domain.AssetType{
	domain.AssetTypeStock,
	domain.AssetTypeFund,
	domain.AssetTypeWealth,
}

// NewMarketQuoteTool 构造 market_quote 工具：按 symbol 查当前用户已记录资产的最新行情。
//
// 实现：
//  1. 从 ctx 取 user_id（D13 规则 1）；缺失即返回 ErrAIToolCallFailed
//  2. 在 stock/fund/wealth 三个 asset_type 上依次尝试 GetByCode(uid, symbol, type)，
//     任一查到即用（spec "查上证指数" 在用户已记录该 symbol 时成立）
//  3. 用 asset.ID 调 Quote.GetLatest 拿最新报价；失败按 spec
//     "底层数据源失败 → ErrAIToolCallFailed.WithMsg(provider=...)"
func NewMarketQuoteTool(deps MarketQuoteDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, args MarketQuoteArgs) (MarketQuoteOutput, error) {
			uid, ok := UserIDFromContext(ctx)
			if !ok {
				return MarketQuoteOutput{}, fmt.Errorf(
					"user_id not in context (provider=ctx_injection): %w",
					errs.ErrAIToolCallFailed,
				)
			}
			symbol := strings.TrimSpace(args.Symbol)
			if symbol == "" {
				return MarketQuoteOutput{}, fmt.Errorf(
					"symbol required: %w",
					errs.ErrAIToolCallFailed,
				)
			}

			// 多 type 兜底
			var asset *domain.Asset
			for _, t := range marketQuoteAssetTypes {
				a, err := deps.Asset.GetByCode(ctx, uid, symbol, t)
				if err == nil && a != nil {
					asset = a
					break
				}
			}
			if asset == nil {
				return MarketQuoteOutput{}, fmt.Errorf(
					"asset not found for symbol %q: this system can only query assets the user has added to their portfolio. Tell the user they need to add this asset first, or ask if they want to check their existing holdings instead (provider=local_asset): %w",
					symbol, errs.ErrAIToolCallFailed,
				)
			}

			quote, err := deps.Quote.GetLatest(ctx, asset.ID)
			if err != nil || quote == nil {
				return MarketQuoteOutput{}, fmt.Errorf(
					"no quote for symbol %q (provider=quote_repo): %w",
					symbol, errs.ErrAIToolCallFailed,
				)
			}

			out := MarketQuoteOutput{
				Symbol:        asset.AssetCode,
				Name:          asset.Name,
				Price:         quote.Price.String(),
				ChangePercent: quote.ChangePct.String(),
				UpdatedAt:     quote.QuoteTime.Format("2006-01-02 15:04:05"),
			}
			return out, nil
		},
		sdkfunction.WithName("market_quote"),
		sdkfunction.WithDescription("查询当前用户已记录资产的最新行情（价格、涨跌幅、报价时间）。symbol 必须是该用户在系统中已存在的资产代码（如 sh000001），不能凭空编造。如果不确定用户持有哪些资产，请先调用 holding_query 或 search_fund 获取合法的 asset_code/symbol，再调用本工具。"),
	)
}
