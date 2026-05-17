package tools_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/llm/tools"
	"github.com/eyjian/fin-vault/backend/pkg/errs"

	"github.com/eyjian/fin-vault/backend/internal/testutil"
)

// callMarketQuote 直接调 sdktool.CallableTool.Call。
func callMarketQuote(t *testing.T, ctx context.Context, deps tools.MarketQuoteDeps, jsonArgs string) (any, error) {
	t.Helper()
	tool := tools.NewMarketQuoteTool(deps)
	return tool.Call(ctx, []byte(jsonArgs))
}

// makeAsset 构造一条用户私有资产；MockAssetRepo.SetAsset 会按
// (UserID, AssetCode, AssetType) 三元组建立 ByCode 索引，供 GetByCode 命中。
func makeAsset(userID uint, code, name string, t domain.AssetType) *domain.Asset {
	a := &domain.Asset{
		UserID:    userID,
		AssetCode: code,
		Name:      name,
		AssetType: t,
	}
	a.ID = 100 // 任意值，仅用于关联 quote
	return a
}

// =====================================================================
// 正常路径
// =====================================================================

// TestMarketQuote_Success_StockType_HitFirstFallback：默认多 type 兜底顺序
// stock → fund → wealth；用户在 stock 类型下记录了 sh000001 时一击命中。
func TestMarketQuote_Success_StockType_HitFirstFallback(t *testing.T) {
	const uid = uint(42)
	mockAsset := testutil.NewMockAssetRepo()
	asset := makeAsset(uid, "sh000001", "上证指数", domain.AssetTypeStock)
	mockAsset.SetAsset(asset)

	mockQuote := testutil.NewMockQuoteRepo()
	mockQuote.Latest[asset.ID] = &domain.PriceQuote{
		AssetID:   asset.ID,
		Price:     decimal.RequireFromString("3145.21"),
		ChangePct: decimal.RequireFromString("0.85"),
		QuoteTime: time.Date(2026, 5, 17, 15, 0, 0, 0, time.UTC),
	}

	ctx := tools.WithUserID(context.Background(), uid)
	out, err := callMarketQuote(t, ctx, tools.MarketQuoteDeps{Quote: mockQuote, Asset: mockAsset}, `{"symbol":"sh000001"}`)
	require.NoError(t, err)

	res, ok := out.(tools.MarketQuoteOutput)
	require.True(t, ok, "返回类型必须是 MarketQuoteOutput, got %T", out)
	assert.Equal(t, "sh000001", res.Symbol)
	assert.Equal(t, "上证指数", res.Name)
	assert.Equal(t, "3145.21", res.Price)
	assert.Equal(t, "0.85", res.ChangePercent)
	assert.Equal(t, "2026-05-17 15:00:00", res.UpdatedAt)
}

// TestMarketQuote_Success_FundType_FallbackOnSecondType：用户记录的同 symbol
// 是 fund 类型；stock 类型 GetByCode 返回 NotFound，兜底到 fund 命中。
//
// 这是 architect 决策的"asset 多 type 兜底"机制核心断言。
func TestMarketQuote_Success_FundType_FallbackOnSecondType(t *testing.T) {
	const uid = uint(7)
	mockAsset := testutil.NewMockAssetRepo()
	// 仅在 fund 类型下注册（stock/wealth 都未注册 → GetByCode 会返回 ErrNotFound）
	asset := makeAsset(uid, "110011", "易方达医疗", domain.AssetTypeFund)
	mockAsset.SetAsset(asset)

	mockQuote := testutil.NewMockQuoteRepo()
	mockQuote.Latest[asset.ID] = &domain.PriceQuote{
		AssetID:   asset.ID,
		Price:     decimal.RequireFromString("2.8501"),
		ChangePct: decimal.RequireFromString("-1.20"),
		QuoteTime: time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC),
	}

	ctx := tools.WithUserID(context.Background(), uid)
	out, err := callMarketQuote(t, ctx, tools.MarketQuoteDeps{Quote: mockQuote, Asset: mockAsset}, `{"symbol":"110011"}`)
	require.NoError(t, err)

	res, ok := out.(tools.MarketQuoteOutput)
	require.True(t, ok)
	assert.Equal(t, "110011", res.Symbol)
	assert.Equal(t, "易方达医疗", res.Name)
	assert.Equal(t, "2.8501", res.Price)
	assert.Equal(t, "-1.2", res.ChangePercent)
}

// =====================================================================
// 失败路径
// =====================================================================

// TestMarketQuote_NoUserIDInCtx_Errors：D13 安全回归——ctx 没注入 user_id
// 时立即返错（不兜底成 user 1 / 不静默查询）。错误信息含 provider=ctx_injection。
func TestMarketQuote_NoUserIDInCtx_Errors(t *testing.T) {
	mockAsset := testutil.NewMockAssetRepo()
	mockQuote := testutil.NewMockQuoteRepo()

	// 显式不注入 ctx user_id
	ctx := context.Background()
	_, ok := tools.UserIDFromContext(ctx)
	require.False(t, ok)

	_, err := callMarketQuote(t, ctx, tools.MarketQuoteDeps{Quote: mockQuote, Asset: mockAsset}, `{"symbol":"sh000001"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user_id not in context")
	assert.Contains(t, err.Error(), "provider=ctx_injection")
	assert.True(t, errors.Is(err, errs.ErrAIToolCallFailed),
		"错误应 wrap ErrAIToolCallFailed，service 层据此映射 SDK 错误")
}

// TestMarketQuote_EmptySymbol_Errors：symbol="" 立即返错。
func TestMarketQuote_EmptySymbol_Errors(t *testing.T) {
	const uid = uint(1)
	ctx := tools.WithUserID(context.Background(), uid)

	_, err := callMarketQuote(t, ctx,
		tools.MarketQuoteDeps{Quote: testutil.NewMockQuoteRepo(), Asset: testutil.NewMockAssetRepo()},
		`{"symbol":""}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbol required")
	assert.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// TestMarketQuote_AssetNotFoundInAnyType_Errors：用户没记录该 symbol（三个 type
// GetByCode 全 NotFound）→ 错误信息含 provider=local_asset。
func TestMarketQuote_AssetNotFoundInAnyType_Errors(t *testing.T) {
	const uid = uint(99)
	ctx := tools.WithUserID(context.Background(), uid)

	mockAsset := testutil.NewMockAssetRepo() // 空，没 SetAsset
	mockQuote := testutil.NewMockQuoteRepo()

	_, err := callMarketQuote(t, ctx, tools.MarketQuoteDeps{Quote: mockQuote, Asset: mockAsset}, `{"symbol":"sh000001"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "asset not found")
	assert.Contains(t, err.Error(), "provider=local_asset")
	assert.Contains(t, err.Error(), "sh000001")
	assert.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// TestMarketQuote_QuoteRepoNoLatest_Errors：asset 找到但 Quote.GetLatest 返回 ErrNotFound
// → 错误信息含 provider=quote_repo（区别于 asset 缺失）。
func TestMarketQuote_QuoteRepoNoLatest_Errors(t *testing.T) {
	const uid = uint(7)
	mockAsset := testutil.NewMockAssetRepo()
	asset := makeAsset(uid, "sh000001", "上证指数", domain.AssetTypeStock)
	mockAsset.SetAsset(asset)

	mockQuote := testutil.NewMockQuoteRepo() // Latest 空，GetLatest 会返回 ErrNotFound

	ctx := tools.WithUserID(context.Background(), uid)
	_, err := callMarketQuote(t, ctx, tools.MarketQuoteDeps{Quote: mockQuote, Asset: mockAsset}, `{"symbol":"sh000001"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no quote")
	assert.Contains(t, err.Error(), "provider=quote_repo")
	assert.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// =====================================================================
// D13 安全回归：用户不能查别人的资产
// =====================================================================

// TestMarketQuote_UserIsolation_OnlySeesOwnAsset：用户 A 注入 ctx，但 mock 里只有
// 用户 B 加过 sh000001 → fn 应当返回 NotFound（不跨用户读取）。
//
// 这是 D13 规则的核心断言：身份从 ctx 取后，repository GetByCode(uid, ...)
// 强制用户隔离过滤。
func TestMarketQuote_UserIsolation_OnlySeesOwnAsset(t *testing.T) {
	const userA = uint(7)
	const userB = uint(42)
	mockAsset := testutil.NewMockAssetRepo()

	// 仅用户 B 加过 sh000001
	bAsset := makeAsset(userB, "sh000001", "上证指数", domain.AssetTypeStock)
	mockAsset.SetAsset(bAsset)

	mockQuote := testutil.NewMockQuoteRepo()
	mockQuote.Latest[bAsset.ID] = &domain.PriceQuote{
		AssetID:   bAsset.ID,
		Price:     decimal.RequireFromString("3145.21"),
		QuoteTime: time.Now(),
	}

	// 用户 A 调用
	ctx := tools.WithUserID(context.Background(), userA)
	_, err := callMarketQuote(t, ctx, tools.MarketQuoteDeps{Quote: mockQuote, Asset: mockAsset}, `{"symbol":"sh000001"}`)
	require.Error(t, err, "用户 A 不应能读取用户 B 的资产")
	assert.Contains(t, err.Error(), "asset not found")
}

// =====================================================================
// 工具元信息
// =====================================================================

func TestMarketQuote_Declaration(t *testing.T) {
	tool := tools.NewMarketQuoteTool(tools.MarketQuoteDeps{})
	require.NotNil(t, tool)
	d := tool.Declaration()
	require.NotNil(t, d)
	assert.Equal(t, "market_quote", d.Name)
	assert.NotEmpty(t, d.Description)
	assert.Contains(t, d.Description, "symbol")
}
