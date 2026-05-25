package service

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// HoldingService.List 测试
// =====================================================================

func TestHoldingService_List_Empty_ReturnsEmpty(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	quoteRepo := testutil.NewMockQuoteRepo()
	rateRepo := testutil.NewMockRateRepo()
	svc := NewHoldingService(holdingRepo, assetRepo, quoteRepo, rateRepo, &mockPlatformRepo{})
	views, total, err := svc.List(context.Background(), HoldingListInput{
		UserID: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, views, 0)
}

func TestHoldingService_List_WithHoldings_ReturnsViews(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	quoteRepo := testutil.NewMockQuoteRepo()

	asset := &domain.Asset{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	}
	assetRepo.SetAsset(asset)

	holding := &domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(100),
		TotalCost:  decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(10.0),
		Status:     domain.HoldingStatusHolding,
	}
	holding.Asset = asset
	holdingRepo.SetHolding(holding)

	quoteRepo.Latest[1] = &domain.PriceQuote{
		AssetID: 1,
		Price:   decimal.NewFromFloat(12.0),
	}

	svc := NewHoldingService(holdingRepo, assetRepo, quoteRepo, testutil.NewMockRateRepo(), &mockPlatformRepo{})
	views, total, err := svc.List(context.Background(), HoldingListInput{
		UserID: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, views, 1)
	assert.True(t, views[0].MarketValue.GreaterThan(decimal.Zero))
}

func TestHoldingService_List_FilterByAssetType(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()

	asset := &domain.Asset{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	}
	assetRepo.SetAsset(asset)

	holding := &domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(100),
		TotalCost:  decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(10.0),
		Status:     domain.HoldingStatusHolding,
	}
	holding.Asset = asset
	holdingRepo.SetHolding(holding)

	svc := NewHoldingService(holdingRepo, assetRepo, testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	views, total, err := svc.List(context.Background(), HoldingListInput{
		UserID:    1,
		AssetType: domain.AssetTypeFund,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, views, 1)
}

// =====================================================================
// HoldingService.Get 测试
// =====================================================================

func TestHoldingService_Get_NotFound_ReturnsErrHoldingNotFound(t *testing.T) {
	svc := NewHoldingService(testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	_, err := svc.Get(context.Background(), 1, 999)
	require.Error(t, err)
	assert.Equal(t, errs.ErrHoldingNotFound.Code, errs.As(err).Code)
}

func TestHoldingService_Get_HappyPath(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()

	asset := &domain.Asset{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	}
	assetRepo.SetAsset(asset)

	holding := &domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(100),
		TotalCost:  decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(10.0),
		Status:     domain.HoldingStatusHolding,
	}
	holding.Asset = asset
	holdingRepo.SetHolding(holding)

	quoteRepo := testutil.NewMockQuoteRepo()
	quoteRepo.Latest[1] = &domain.PriceQuote{
		AssetID: 1,
		Price:   decimal.NewFromFloat(12.0),
	}

	svc := NewHoldingService(holdingRepo, assetRepo, quoteRepo, testutil.NewMockRateRepo(), &mockPlatformRepo{})
	view, err := svc.Get(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.NotNil(t, view)
	assert.True(t, view.MarketValue.GreaterThan(decimal.Zero))
}

// =====================================================================
// HoldingService.SwitchCostMethod 测试
// =====================================================================

func TestHoldingService_SwitchCostMethod_InvalidMethod_ReturnsInvalidParam(t *testing.T) {
	svc := NewHoldingService(testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	err := svc.SwitchCostMethod(context.Background(), 1, 1, domain.CostMethod("invalid"))
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestHoldingService_SwitchCostMethod_NotFound_ReturnsErrHoldingNotFound(t *testing.T) {
	svc := NewHoldingService(testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	err := svc.SwitchCostMethod(context.Background(), 1, 999, domain.CostMethodWeightedAvg)
	require.Error(t, err)
	assert.Equal(t, errs.ErrHoldingNotFound.Code, errs.As(err).Code)
}

func TestHoldingService_SwitchCostMethod_HappyPath(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	holding := &domain.Holding{
		BaseModel:    domain.BaseModel{ID: 1},
		UserID:       1,
		AssetID:      1,
		PlatformID:   1,
		Quantity:      decimal.NewFromInt(100),
		TotalCost:    decimal.NewFromInt(1000),
		AvgCost:       decimal.NewFromFloat(10.0),
		Status:        domain.HoldingStatusHolding,
		CostMethod:    domain.CostMethodWeightedAvg,
	}
	holdingRepo.SetHolding(holding)

	svc := NewHoldingService(holdingRepo, testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	err := svc.SwitchCostMethod(context.Background(), 1, 1, domain.CostMethodFIFO)
	require.NoError(t, err)

	updated, _ := holdingRepo.GetByID(context.Background(), 1, 1)
	assert.Equal(t, domain.CostMethodFIFO, updated.CostMethod)
}

// =====================================================================
// HoldingService.Summary 测试
// =====================================================================

func TestHoldingService_Summary_Empty_ReturnsZeroSummary(t *testing.T) {
	svc := NewHoldingService(testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	summary, err := svc.Summary(context.Background(), 1, "raw")
	require.NoError(t, err)
	assert.True(t, summary.TotalMarketValue.IsZero())
	assert.True(t, summary.TotalCost.IsZero())
	assert.True(t, summary.TotalPnL.IsZero())
	assert.Len(t, summary.ByType, 0)
	assert.Len(t, summary.ByPlatform, 0)
	assert.Len(t, summary.ByCurrency, 0)
}

func TestHoldingService_Summary_WithHoldings_ReturnsCorrectSummary(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	quoteRepo := testutil.NewMockQuoteRepo()

	asset := &domain.Asset{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	}
	assetRepo.SetAsset(asset)

	holding := &domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(100),
		TotalCost:  decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(10.0),
		Status:     domain.HoldingStatusHolding,
	}
	holding.Asset = asset
	holdingRepo.SetHolding(holding)

	quoteRepo.Latest[1] = &domain.PriceQuote{
		AssetID: 1,
		Price:   decimal.NewFromFloat(12.0),
	}

	svc := NewHoldingService(holdingRepo, assetRepo, quoteRepo, testutil.NewMockRateRepo(), &mockPlatformRepo{})
	summary, err := svc.Summary(context.Background(), 1, "raw")
	require.NoError(t, err)
	assert.True(t, summary.TotalMarketValue.GreaterThan(decimal.Zero))
	assert.True(t, summary.TotalCost.GreaterThan(decimal.Zero))
	assert.Len(t, summary.ByType, 1)
	assert.Len(t, summary.ByPlatform, 1)
}

func TestHoldingService_Summary_CNYDisplayCurrency_ConvertsCurrency(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	quoteRepo := testutil.NewMockQuoteRepo()
	rateRepo := testutil.NewMockRateRepo()

	asset := &domain.Asset{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
		Currency:  "USD",
	}
	assetRepo.SetAsset(asset)

	holding := &domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(100),
		TotalCost:  decimal.NewFromInt(1000),
		AvgCost:    decimal.NewFromFloat(10.0),
		Status:     domain.HoldingStatusHolding,
	}
	holding.Asset = asset
	holdingRepo.SetHolding(holding)

	quoteRepo.Latest[1] = &domain.PriceQuote{
		AssetID: 1,
		Price:   decimal.NewFromFloat(12.0),
	}

	rateRepo.SetLatest("USD", "CNY", &domain.ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromFloat(7.2),
		QuoteDate:    time.Now(),
		Source:       domain.RateSourceManual,
	})

	svc := NewHoldingService(holdingRepo, assetRepo, quoteRepo, rateRepo, &mockPlatformRepo{})
	summary, err := svc.Summary(context.Background(), 1, "CNY")
	require.NoError(t, err)
	assert.True(t, summary.TotalMarketValue.GreaterThan(decimal.Zero))
}

// =====================================================================
// HoldingService.buildView 测试
// =====================================================================

func TestHoldingService_buildView_NoQuote_ZeroMarketValue(t *testing.T) {
	svc := NewHoldingService(testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	h := &domain.Holding{
		BaseModel:      domain.BaseModel{ID: 1},
		Quantity:       decimal.NewFromInt(100),
		TotalCost:     decimal.NewFromInt(1000),
		RealizedPnL:  decimal.Zero,
		TotalDividend: decimal.Zero,
	}
	view := svc.buildView(h, nil)
	assert.True(t, view.MarketValue.IsZero())
	assert.Equal(t, decimal.NewFromInt(-1000).Round(2), view.UnrealizedPnL)
}

func TestHoldingService_buildView_WithQuote_CalculatesMarketValue(t *testing.T) {
	svc := NewHoldingService(testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo(), testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), &mockPlatformRepo{})
	h := &domain.Holding{
		BaseModel:      domain.BaseModel{ID: 1},
		Quantity:       decimal.NewFromInt(100),
		TotalCost:      decimal.NewFromInt(1000),
		RealizedPnL:   decimal.Zero,
		TotalDividend:  decimal.Zero,
	}
	q := &domain.PriceQuote{
		Price: decimal.NewFromFloat(12.0),
	}
	view := svc.buildView(h, q)
	assert.True(t, view.MarketValue.Equal(decimal.NewFromInt(1200)))
	assert.True(t, view.UnrealizedPnL.Equal(decimal.NewFromInt(200)))
}
