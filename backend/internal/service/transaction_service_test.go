package service

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// TransactionService.Create 参数校验
// =====================================================================

func TestTransactionService_Create_UserIDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		AssetID:    1,
		PlatformID: 1,
		TxnType:    domain.TxnTypeBuy,
		Quantity:   decimal.NewFromInt(100),
		Price:      decimal.NewFromFloat(10.0),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestTransactionService_Create_AssetIDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		UserID:    1,
		PlatformID: 1,
		TxnType:   domain.TxnTypeBuy,
		Quantity:   decimal.NewFromInt(100),
		Price:      decimal.NewFromFloat(10.0),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestTransactionService_Create_PlatformIDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		UserID:   1,
		AssetID:  1,
		TxnType:  domain.TxnTypeBuy,
		Quantity:  decimal.NewFromInt(100),
		Price:     decimal.NewFromFloat(10.0),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestTransactionService_Create_InvalidTxnType_ReturnsErrTxnTypeInvalid(t *testing.T) {
	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		TxnType:   domain.TxnType("invalid"),
		Quantity:   decimal.NewFromInt(100),
		Price:      decimal.NewFromFloat(10.0),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrTxnTypeInvalid.Code, errs.As(err).Code)
}

func TestTransactionService_Create_AdjustWithoutNote_ReturnsInvalidParam(t *testing.T) {
	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		TxnType:   domain.TxnTypeAdjust,
		Quantity:   decimal.NewFromInt(100),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestTransactionService_Create_QuantityZero_ReturnsErrTxnQuantityInvalid(t *testing.T) {
	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		TxnType:   domain.TxnTypeBuy,
		Quantity:   decimal.Zero,
		Price:      decimal.NewFromFloat(10.0),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrTxnQuantityInvalid.Code, errs.As(err).Code)
}

func TestTransactionService_Create_PriceZero_ReturnsErrTxnPriceInvalid(t *testing.T) {
	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), testutil.NewMockHoldingRepo(), testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		TxnType:   domain.TxnTypeBuy,
		Quantity:   decimal.NewFromInt(100),
		Price:      decimal.Zero,
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrTxnPriceInvalid.Code, errs.As(err).Code)
}

func TestTransactionService_Create_SellInsufficientQuantity_ReturnsErrInsufficientQuantity(t *testing.T) {
	holdingRepo := testutil.NewMockHoldingRepo()
	holdingRepo.SetHolding(&domain.Holding{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(50),
		TotalCost:  decimal.NewFromInt(500),
		AvgCost:    decimal.NewFromFloat(10.0),
		Status:     domain.HoldingStatusHolding,
	})

	svc := NewTransactionService(&testutil.MockUoW{}, testutil.NewMockTransactionRepo(), holdingRepo, testutil.NewMockAssetRepo())
	_, err := svc.Create(context.Background(), CreateTxnInput{
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		TxnType:   domain.TxnTypeSell,
		Quantity:   decimal.NewFromInt(100),
		Price:      decimal.NewFromFloat(12.0),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInsufficientQuantity.Code, errs.As(err).Code)
}
