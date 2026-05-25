package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// Test_MarketData_Success — 批量行情 + asset 元信息拼接
// =====================================================================
func Test_MarketData_Success(t *testing.T) {
	uid := uint(7)
	quoteRepo := testutil.NewMockQuoteRepo()
	assetRepo := testutil.NewMockAssetRepo()

	assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    uid,
		AssetCode: "510300",
		Name:      "沪深300ETF",
		AssetType: domain.AssetTypeFund,
	})
	require.NoError(t, quoteRepo.Insert(context.Background(), &domain.PriceQuote{
		AssetID:   100,
		Price:     decimal.NewFromFloat(4.123),
		ChangePct: decimal.NewFromFloat(0.0123),
		QuoteTime: time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC),
		Source:    "tianapi",
	}))

	tool := NewMarketDataTool(MarketDataDeps{Quote: quoteRepo, Asset: assetRepo})
	ctx := WithUserID(context.Background(), uid)

	out, err := callTool(ctx, t, tool, MarketDataArgs{AssetIDs: []uint{100}})
	require.NoError(t, err)
	require.Contains(t, out, `"count":1`)
	require.Contains(t, out, `"price":"4.123"`)
	require.Contains(t, out, `"asset_code":"510300"`)
	require.Contains(t, out, `"source":"tianapi"`)
}

// =====================================================================
// Test_MarketData_EmptyAssetIDs — 空入参快速返回 0 条
// =====================================================================
func Test_MarketData_EmptyAssetIDs(t *testing.T) {
	tool := NewMarketDataTool(MarketDataDeps{
		Quote: testutil.NewMockQuoteRepo(),
		Asset: testutil.NewMockAssetRepo(),
	})
	ctx := WithUserID(context.Background(), 7)
	out, err := callTool(ctx, t, tool, MarketDataArgs{AssetIDs: nil})
	require.NoError(t, err)
	require.Contains(t, out, `"count":0`)
}

// =====================================================================
// Test_MarketData_QuoteMiss_Skipped — 行情未命中 asset 应被跳过，不报错
// =====================================================================
func Test_MarketData_QuoteMiss_Skipped(t *testing.T) {
	tool := NewMarketDataTool(MarketDataDeps{
		Quote: testutil.NewMockQuoteRepo(), // 空，所有 id 都未命中
		Asset: testutil.NewMockAssetRepo(),
	})
	ctx := WithUserID(context.Background(), 7)
	out, err := callTool(ctx, t, tool, MarketDataArgs{AssetIDs: []uint{1, 2, 3}})
	require.NoError(t, err)
	require.Contains(t, out, `"count":0`)
}

// =====================================================================
// Test_MarketData_NoUserIDInCtx_Errors — D13 安全回归
// =====================================================================
func Test_MarketData_NoUserIDInCtx_Errors(t *testing.T) {
	tool := NewMarketDataTool(MarketDataDeps{
		Quote: testutil.NewMockQuoteRepo(),
		Asset: testutil.NewMockAssetRepo(),
	})
	_, err := callTool(context.Background(), t, tool, MarketDataArgs{AssetIDs: []uint{1}})
	require.Error(t, err)
	require.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// =====================================================================
// Test_MarketData_RepoError — BatchGetLatest 错误透传 wrap
// =====================================================================
//
// 注：testutil.MockQuoteRepo.BatchGetLatest 简化实现不返回 error，所以这里用本地
// fake 注入 error。
type erroringQuoteRepo struct {
	*testutil.MockQuoteRepo
	batchErr error
}

func (e *erroringQuoteRepo) BatchGetLatest(_ context.Context, _ []uint) (map[uint]*domain.PriceQuote, error) {
	if e.batchErr != nil {
		return nil, e.batchErr
	}
	return map[uint]*domain.PriceQuote{}, nil
}

func Test_MarketData_BatchGetLatestError(t *testing.T) {
	quoteRepo := &erroringQuoteRepo{
		MockQuoteRepo: testutil.NewMockQuoteRepo(),
		batchErr:      errors.New("upstream rate-limited"),
	}
	tool := NewMarketDataTool(MarketDataDeps{Quote: quoteRepo, Asset: testutil.NewMockAssetRepo()})
	ctx := WithUserID(context.Background(), 7)

	_, err := callTool(ctx, t, tool, MarketDataArgs{AssetIDs: []uint{1}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "get latest batch failed")
}

// =====================================================================
// Test_MarketData_NoUserIDFieldInSchema — 防御性 schema 断言
// =====================================================================
func Test_MarketData_NoUserIDFieldInSchema(t *testing.T) {
	tool := NewMarketDataTool(MarketDataDeps{
		Quote: testutil.NewMockQuoteRepo(),
		Asset: testutil.NewMockAssetRepo(),
	})
	decl := tool.Declaration()
	require.NotNil(t, decl.InputSchema)
	require.Equal(t, "market_data", decl.Name)
	for k := range decl.InputSchema.Properties {
		lower := strings.ToLower(k)
		require.NotEqual(t, "user_id", lower)
		require.NotEqual(t, "userid", lower)
	}
	b, err := json.Marshal(decl.InputSchema)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(b)), `"user_id"`)
	require.NotContains(t, strings.ToLower(string(b)), `"userid"`)
}
