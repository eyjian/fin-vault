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
// Test_ProfitCalc_Success — 持仓 + 行情 → 盈亏汇总
// =====================================================================
func Test_ProfitCalc_Success(t *testing.T) {
	uid := uint(7)
	holdingRepo := testutil.NewMockHoldingRepo()
	quoteRepo := testutil.NewMockQuoteRepo()

	// 持仓：1000 份，平均成本 3.5，total_cost 3500
	holdingRepo.SetHolding(&domain.Holding{
		BaseModel:     domain.BaseModel{ID: 1},
		UserID:        uid,
		AssetID:       100,
		PlatformID:    5,
		Quantity:      decimal.NewFromInt(1000),
		AvgCost:       decimal.NewFromFloat(3.5),
		TotalCost:     decimal.NewFromInt(3500),
		RealizedPnL:   decimal.NewFromInt(50),
		TotalDividend: decimal.NewFromInt(20),
		Status:        domain.HoldingStatusHolding,
	})
	// 最新价 4.0 → 市值 4000，未实现 +500，总盈亏 = 500 + 50 + 20 = 570
	require.NoError(t, quoteRepo.Insert(context.Background(), &domain.PriceQuote{
		AssetID:   100,
		Price:     decimal.NewFromInt(4),
		QuoteTime: time.Now(),
		Source:    "test",
	}))

	tool := NewProfitCalcTool(ProfitCalcDeps{Holding: holdingRepo, Quote: quoteRepo})
	ctx := WithUserID(context.Background(), uid)

	out, err := callTool(ctx, t, tool, ProfitCalcArgs{AssetType: "fund"})
	require.NoError(t, err)
	require.Contains(t, out, `"total_cost":"3500"`)
	require.Contains(t, out, `"total_market_value":"4000"`)
	// total_pnl = unreal(500) + realized(50) + dividend(20) = 570
	require.Contains(t, out, `"total_pnl":"570"`)
	require.Contains(t, out, `"holding_id":1`)
	require.Contains(t, out, `"latest_price":"4"`)
}

// =====================================================================
// Test_ProfitCalc_NoHoldings_ZeroOutput — 边界：无持仓时 ratio 为 0 不 panic
// =====================================================================
func Test_ProfitCalc_NoHoldings_ZeroOutput(t *testing.T) {
	uid := uint(7)
	holdingRepo := testutil.NewMockHoldingRepo()
	quoteRepo := testutil.NewMockQuoteRepo()

	tool := NewProfitCalcTool(ProfitCalcDeps{Holding: holdingRepo, Quote: quoteRepo})
	ctx := WithUserID(context.Background(), uid)

	out, err := callTool(ctx, t, tool, ProfitCalcArgs{})
	require.NoError(t, err)
	require.Contains(t, out, `"total_cost":"0"`)
	require.Contains(t, out, `"pnl_ratio":"0"`)
}

// =====================================================================
// Test_ProfitCalc_RepoError
// =====================================================================
func Test_ProfitCalc_RepoError(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	quoteRepo := testutil.NewMockQuoteRepo()
	holdingRepo.ListByUserErr = errors.New("db down")

	tool := NewProfitCalcTool(ProfitCalcDeps{Holding: holdingRepo, Quote: quoteRepo})
	ctx := WithUserID(context.Background(), 7)

	_, err := callTool(ctx, t, tool, ProfitCalcArgs{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list holdings failed")
}

// =====================================================================
// Test_ProfitCalc_NoUserIDInCtx_Errors — D13 安全回归
// =====================================================================
func Test_ProfitCalc_NoUserIDInCtx_Errors(t *testing.T) {
	tool := NewProfitCalcTool(ProfitCalcDeps{
		Holding: testutil.NewMockHoldingRepo(),
		Quote:   testutil.NewMockQuoteRepo(),
	})

	_, err := callTool(context.Background(), t, tool, ProfitCalcArgs{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errs.ErrAIToolCallFailed))
}

// =====================================================================
// Test_ProfitCalc_NoUserIDFieldInSchema — 防御性 schema 断言
// =====================================================================
func Test_ProfitCalc_NoUserIDFieldInSchema(t *testing.T) {
	tool := NewProfitCalcTool(ProfitCalcDeps{
		Holding: testutil.NewMockHoldingRepo(),
		Quote:   testutil.NewMockQuoteRepo(),
	})
	decl := tool.Declaration()
	require.NotNil(t, decl.InputSchema)
	for k := range decl.InputSchema.Properties {
		require.NotContains(t, strings.ToLower(k), "user")
	}
	b, err := json.Marshal(decl.InputSchema)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(b)), `"user_id"`)
}
