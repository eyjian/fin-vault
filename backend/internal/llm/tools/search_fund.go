package tools

import (
	"context"
	"fmt"
	"strings"

	sdktool "trpc.group/trpc-go/trpc-agent-go/tool"
	sdkfunction "trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
)

// SearchFundDeps search_fund 工具依赖。
type SearchFundDeps struct {
	Asset repository.AssetRepository
}

// SearchFundArgs LLM 传入参数。
//
// 设计要点（D13 规则 1）：search_fund 是公共数据查询，**不涉及用户隔离**，
// 入参 schema 不含 user_id；fn 内部不读 ctx user_id。
type SearchFundArgs struct {
	Keyword string `json:"keyword" jsonschema:"description=基金名称关键字或基金代码（如\"医药\"或\"110011\"）,required"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=返回条数上限，默认 20，最大 20"`
}

// SearchFundOutput LLM 可见的返回。
type SearchFundOutput struct {
	Items []SearchFundItem `json:"items" jsonschema:"description=匹配的基金列表"`
	Count int              `json:"count" jsonschema:"description=结果条数"`
}

// SearchFundItem 单条基金摘要（不含价格信息——市场报价由 market_quote 工具单独提供）。
type SearchFundItem struct {
	Code string `json:"code" jsonschema:"description=基金代码"`
	Name string `json:"name" jsonschema:"description=基金名称"`
	Type string `json:"type" jsonschema:"description=资产类型，固定为 fund"`
}

// searchFundMaxLimit 是 spec ai-tools "search_fund 模糊匹配 ≤ 20 条" 的硬上限。
const searchFundMaxLimit = 20

// NewSearchFundTool 构造 search_fund 工具：按 keyword 模糊匹配 fund 资产。
//
// 公共数据查询（不需 D13 ctx user_id 隔离）。底层用
// AssetRepository.List + Filters{"asset_type": "fund", "keyword": kw}，
// repository 层的 SQL 形如 `f_name LIKE ? OR f_asset_code LIKE ?`，
// 同时按 asset_type=fund 过滤。
//
// UserID = 0 让 List 不附加 user_id WHERE（asset_repo.go L65-67 逻辑）：
// 这里我们检索全库匹配的基金（公共数据语义），与 spec
// "search_fund 用于查找候选基金，不限定持仓用户" 对齐。
func NewSearchFundTool(deps SearchFundDeps) sdktool.CallableTool {
	return sdkfunction.NewFunctionTool(
		func(ctx context.Context, args SearchFundArgs) (SearchFundOutput, error) {
			kw := strings.TrimSpace(args.Keyword)
			if kw == "" {
				return SearchFundOutput{}, fmt.Errorf("keyword required")
			}
			limit := args.Limit
			if limit <= 0 || limit > searchFundMaxLimit {
				limit = searchFundMaxLimit
			}

			opts := repository.ListOptions{
				UserID:   0, // 公共查询，不限用户
				Page:     1,
				PageSize: limit,
				OrderBy:  "f_id desc",
				Filters: map[string]any{
					"asset_type": string(domain.AssetTypeFund),
					"keyword":    kw,
				},
			}
			assets, _, err := deps.Asset.List(ctx, opts)
			if err != nil {
				return SearchFundOutput{}, fmt.Errorf("list funds failed: %w", err)
			}

			items := make([]SearchFundItem, 0, len(assets))
			for i := range assets {
				a := &assets[i]
				items = append(items, SearchFundItem{
					Code: a.AssetCode,
					Name: a.Name,
					Type: string(a.AssetType),
				})
			}
			return SearchFundOutput{Items: items, Count: len(items)}, nil
		},
		sdkfunction.WithName("search_fund"),
		sdkfunction.WithDescription("按 keyword 模糊匹配基金（按基金名称或代码），结果上限 20 条。返回 items[]{code,name,type}。"),
	)
}
