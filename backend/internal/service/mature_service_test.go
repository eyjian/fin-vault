package service

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
)

// spyWealthAssetRepo 包装 MockAssetRepo，允许覆盖 GetWealthDetail 行为，
// 用于直接测试 ErrNotFound 路径而非 nil-return 路径。
type spyWealthAssetRepo struct {
	*testutil.MockAssetRepo
	getWealthDetail func(ctx context.Context, assetID uint) (*domain.WealthDetail, error)
}

func (s *spyWealthAssetRepo) GetWealthDetail(ctx context.Context, assetID uint) (*domain.WealthDetail, error) {
	if s.getWealthDetail != nil {
		return s.getWealthDetail(ctx, assetID)
	}
	return s.MockAssetRepo.GetWealthDetail(ctx, assetID)
}

// =====================================================================
// MatureService.RunOnce
// =====================================================================

// 构造一笔到期的理财持仓 + 对应 wealth detail。
//
// expected_yield = 4%, term_days = 365, total_cost = 10000
// → mature = 10000 * (1 + 4/100 * 365/365) = 10400
// → realized_pnl 增加 400
func TestMatureService_RunOnce_BasicHappyPath(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	txnRepo := testutil.NewMockTransactionRepo()

	endDate := time.Now().AddDate(0, 0, -1) // 昨天到期
	startDate := endDate.AddDate(-1, 0, 0)
	assetID := uint(101)
	holdingID := uint(1001)

	holdingRepo.MaturedList = []domain.Holding{
		{
			BaseModel:   domain.BaseModel{ID: holdingID},
			UserID:      1,
			AssetID:     assetID,
			PlatformID:  10,
			Quantity:    decimal.RequireFromString("10000"),
			AvgCost:     decimal.RequireFromString("1"),
			TotalCost:   decimal.RequireFromString("10000"),
			RealizedPnL: decimal.Zero,
			Status:      domain.HoldingStatusHolding,
			CostMethod:  domain.CostMethodWeightedAvg,
		},
	}
	_ = assetRepo.UpsertWealthDetail(context.Background(), &domain.WealthDetail{
		AssetID:       assetID,
		ProductType:   "fixed_deposit",
		ExpectedYield: decimal.RequireFromString("4"), // 4% 年化
		TermDays:      365,
		StartDate:     &startDate,
		EndDate:       &endDate,
	})

	svc := NewMatureService(uow, holdingRepo, assetRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, stat.Scanned)
	assert.Equal(t, 1, stat.Matured)
	assert.Equal(t, 0, stat.Skipped)
	assert.Empty(t, stat.Errors)

	// 1) 写了一笔 mature 流水
	require.Len(t, txnRepo.Inserts, 1)
	mature := txnRepo.Inserts[0]
	assert.Equal(t, domain.TxnTypeMature, mature.TxnType)
	assert.Equal(t, domain.TxnSourceAutoMature, mature.Source)
	assert.Equal(t, holdingID, mature.HoldingID)
	assert.True(t, mature.NetAmount.Equal(decimal.RequireFromString("10400")),
		"mature net=%s, want 10400", mature.NetAmount)
	assert.True(t, mature.Amount.Equal(mature.NetAmount))
	assert.True(t, mature.Fee.IsZero())
	assert.True(t, mature.Tax.IsZero())

	// 2) 持仓被更新：status=matured, quantity=0, realized_pnl += 400
	require.Len(t, holdingRepo.Updates, 1)
	updated := holdingRepo.Updates[0]
	assert.Equal(t, domain.HoldingStatusMatured, updated.Status)
	assert.True(t, updated.Quantity.IsZero())
	assert.True(t, updated.RealizedPnL.Equal(decimal.RequireFromString("400")),
		"realized_pnl=%s, want 400", updated.RealizedPnL)
	require.NotNil(t, updated.LastTxnAt)

	// 3) 事务被调用了 1 次
	assert.Equal(t, 1, uow.Calls)
}

// 已 matured 的应被跳过（统计计入 Skipped，不写流水/不更新）。
func TestMatureService_RunOnce_AlreadyMatured_Skip(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	txnRepo := testutil.NewMockTransactionRepo()

	holdingRepo.MaturedList = []domain.Holding{
		{
			BaseModel:  domain.BaseModel{ID: 1},
			UserID:     1, AssetID: 101, PlatformID: 10,
			TotalCost:  decimal.RequireFromString("10000"),
			Quantity:   decimal.RequireFromString("10000"),
			Status:     domain.HoldingStatusMatured, // 已经到期过
			CostMethod: domain.CostMethodWeightedAvg,
		},
	}

	svc := NewMatureService(uow, holdingRepo, assetRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, stat.Scanned)
	assert.Equal(t, 0, stat.Matured)
	assert.Equal(t, 1, stat.Skipped)
	assert.Len(t, txnRepo.Inserts, 0)
	assert.Len(t, holdingRepo.Updates, 0)
	assert.Equal(t, 0, uow.Calls)
}

// 缺 wealth_detail 时 errors 计数 +1，不阻断后续。
func TestMatureService_RunOnce_MissingWealthDetail_RecordsError(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo() // 没注入 wealth detail
	txnRepo := testutil.NewMockTransactionRepo()

	holdingRepo.MaturedList = []domain.Holding{
		{
			BaseModel:  domain.BaseModel{ID: 7},
			UserID:     1, AssetID: 999, PlatformID: 10,
			TotalCost:  decimal.RequireFromString("5000"),
			Quantity:   decimal.RequireFromString("5000"),
			Status:     domain.HoldingStatusHolding,
			CostMethod: domain.CostMethodWeightedAvg,
		},
	}

	svc := NewMatureService(uow, holdingRepo, assetRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, stat.Scanned)
	assert.Equal(t, 0, stat.Matured)
	assert.Len(t, stat.Errors, 1)
	// 现在归一到 errs.ErrWealthDetailMissing(30401)，错误字符串包含 "[30401]" 前缀
	assert.Contains(t, stat.Errors[0], "30401")
	assert.Contains(t, stat.Errors[0], "wealth detail")
}

// 直接测 ErrNotFound 路径：assetRepo 返回 (nil, repository.ErrNotFound) → 30401
func TestMatureService_RunOnce_RepoReturnsNotFound_RecordsErr30401(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	// 用一个 spy assetRepo，强制 GetWealthDetail 返回 ErrNotFound
	spyRepo := &spyWealthAssetRepo{
		MockAssetRepo:    testutil.NewMockAssetRepo(),
		getWealthDetail:  func(_ context.Context, _ uint) (*domain.WealthDetail, error) { return nil, repository.ErrNotFound },
	}
	txnRepo := testutil.NewMockTransactionRepo()

	holdingRepo.MaturedList = []domain.Holding{
		{
			BaseModel:  domain.BaseModel{ID: 8},
			UserID:     1, AssetID: 999, PlatformID: 10,
			TotalCost:  decimal.RequireFromString("5000"),
			Quantity:   decimal.RequireFromString("5000"),
			Status:     domain.HoldingStatusHolding,
			CostMethod: domain.CostMethodWeightedAvg,
		},
	}

	svc := NewMatureService(uow, holdingRepo, spyRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Len(t, stat.Errors, 1)
	assert.Contains(t, stat.Errors[0], "30401")
	assert.Contains(t, stat.Errors[0], "not found")
	assert.Equal(t, 0, uow.Calls, "事务不应被调用")
}

// 缺 EndDate 的 wealth detail → 30401
func TestMatureService_RunOnce_MissingEndDate_RecordsErr30401(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	txnRepo := testutil.NewMockTransactionRepo()

	holdingRepo.MaturedList = []domain.Holding{
		{
			BaseModel:  domain.BaseModel{ID: 9},
			UserID:     1, AssetID: 555, PlatformID: 10,
			TotalCost:  decimal.RequireFromString("1000"),
			Quantity:   decimal.RequireFromString("1000"),
			Status:     domain.HoldingStatusHolding,
			CostMethod: domain.CostMethodWeightedAvg,
		},
	}
	// 注入 detail 但没设置 EndDate
	_ = assetRepo.UpsertWealthDetail(context.Background(), &domain.WealthDetail{
		AssetID:       555,
		ExpectedYield: decimal.RequireFromString("4"),
		TermDays:      365,
		// EndDate: nil
	})

	svc := NewMatureService(uow, holdingRepo, assetRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Len(t, stat.Errors, 1)
	assert.Contains(t, stat.Errors[0], "30401")
	assert.Contains(t, stat.Errors[0], "end_date")
}

// ActualYield 优先于 ExpectedYield。
func TestMatureService_RunOnce_ActualYieldOverridesExpected(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	txnRepo := testutil.NewMockTransactionRepo()

	endDate := time.Now().AddDate(0, 0, -1)
	holdingRepo.MaturedList = []domain.Holding{
		{
			BaseModel:  domain.BaseModel{ID: 1},
			UserID:     1, AssetID: 200, PlatformID: 10,
			Quantity:   decimal.RequireFromString("10000"),
			TotalCost:  decimal.RequireFromString("10000"),
			Status:     domain.HoldingStatusHolding,
			CostMethod: domain.CostMethodWeightedAvg,
		},
	}
	_ = assetRepo.UpsertWealthDetail(context.Background(), &domain.WealthDetail{
		AssetID:       200,
		ExpectedYield: decimal.RequireFromString("3"), // 不应被使用
		ActualYield:   decimal.RequireFromString("5"), // 实际 5%
		TermDays:      365,
		EndDate:       &endDate,
	})

	svc := NewMatureService(uow, holdingRepo, assetRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, stat.Matured)

	require.Len(t, txnRepo.Inserts, 1)
	// 10000 * (1 + 5/100 * 1) = 10500
	assert.True(t, txnRepo.Inserts[0].NetAmount.Equal(decimal.RequireFromString("10500")),
		"actual yield should win, got %s", txnRepo.Inserts[0].NetAmount)
}

// 多条混合：1 条已 matured 跳过、1 条 happy、1 条缺 detail 报错。
func TestMatureService_RunOnce_MixedBatch(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	assetRepo := testutil.NewMockAssetRepo()
	txnRepo := testutil.NewMockTransactionRepo()

	endDate := time.Now().AddDate(0, 0, -1)
	holdingRepo.MaturedList = []domain.Holding{
		{ // 已 matured，跳过
			BaseModel:  domain.BaseModel{ID: 1},
			UserID:     1, AssetID: 100, PlatformID: 10,
			Status:     domain.HoldingStatusMatured,
			CostMethod: domain.CostMethodWeightedAvg,
		},
		{ // happy
			BaseModel:  domain.BaseModel{ID: 2},
			UserID:     1, AssetID: 200, PlatformID: 10,
			Quantity:   decimal.RequireFromString("10000"),
			TotalCost:  decimal.RequireFromString("10000"),
			Status:     domain.HoldingStatusHolding,
			CostMethod: domain.CostMethodWeightedAvg,
		},
		{ // 缺 detail
			BaseModel:  domain.BaseModel{ID: 3},
			UserID:     1, AssetID: 300, PlatformID: 10,
			Quantity:   decimal.RequireFromString("8000"),
			TotalCost:  decimal.RequireFromString("8000"),
			Status:     domain.HoldingStatusHolding,
			CostMethod: domain.CostMethodWeightedAvg,
		},
	}
	_ = assetRepo.UpsertWealthDetail(context.Background(), &domain.WealthDetail{
		AssetID:       200,
		ExpectedYield: decimal.RequireFromString("4"),
		TermDays:      365,
		EndDate:       &endDate,
	})

	svc := NewMatureService(uow, holdingRepo, assetRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 3, stat.Scanned)
	assert.Equal(t, 1, stat.Matured)
	assert.Equal(t, 1, stat.Skipped)
	assert.Len(t, stat.Errors, 1)

	require.Len(t, txnRepo.Inserts, 1)
	require.Len(t, holdingRepo.Updates, 1)
	assert.Equal(t, uint(2), holdingRepo.Updates[0].ID)
	assert.Equal(t, domain.HoldingStatusMatured, holdingRepo.Updates[0].Status)
}

// ListMaturedWealth 报错 → RunOnce 返回错误（Scanned=0），不调用事务。
func TestMatureService_RunOnce_ListErr_ReturnsBubbledErr(t *testing.T) {
	uow := &testutil.MockUoW{}
	holdingRepo := testutil.NewMockHoldingRepo()
	holdingRepo.MaturedListErr = assert.AnError
	assetRepo := testutil.NewMockAssetRepo()
	txnRepo := testutil.NewMockTransactionRepo()

	svc := NewMatureService(uow, holdingRepo, assetRepo, txnRepo)
	stat, err := svc.RunOnce(context.Background())
	require.Error(t, err)
	assert.Equal(t, 0, stat.Scanned)
	assert.Equal(t, 0, uow.Calls)
}
