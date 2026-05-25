package service

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// mockPlatformRepo 简易 PlatformRepository 实现，用于测试
// =====================================================================

type mockPlatformRepo struct {
	platforms []domain.Platform
}

func (m *mockPlatformRepo) List(ctx context.Context) ([]domain.Platform, error) {
	return m.platforms, nil
}
func (m *mockPlatformRepo) GetByID(ctx context.Context, id uint) (*domain.Platform, error) {
	for _, p := range m.platforms {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (m *mockPlatformRepo) GetByCode(ctx context.Context, code string) (*domain.Platform, error) {
	for _, p := range m.platforms {
		if p.Code == code {
			return &p, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (m *mockPlatformRepo) Create(ctx context.Context, p *domain.Platform) error {
	m.platforms = append(m.platforms, *p)
	return nil
}
func (m *mockPlatformRepo) Update(ctx context.Context, p *domain.Platform) error {
	return nil
}
func (m *mockPlatformRepo) Delete(ctx context.Context, id uint) error {
	return nil
}

// =====================================================================
// AssetService.Create 参数校验
// =====================================================================

func TestAssetService_Create_UserIDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Create(context.Background(), CreateAssetInput{
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestAssetService_Create_InvalidAssetType_ReturnsErrAssetTypeInvalid(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetType("invalid"),
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrAssetTypeInvalid.Code, errs.As(err).Code)
}

func TestAssetService_Create_EmptyAssetCode_ReturnsInvalidParam(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestAssetService_Create_CashTypeInvalidCode_ReturnsErrCashCodeInvalid(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "INVALID",
		Name:      "Cash Account",
		AssetType: domain.AssetTypeCash,
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrCashCodeInvalid.Code, errs.As(err).Code)
}

func TestAssetService_Create_WealthTypeWithoutDetail_ReturnsErrWealthDetailMissing(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "WM001",
		Name:      "Wealth Product",
		AssetType: domain.AssetTypeWealth,
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrWealthDetailMissing.Code, errs.As(err).Code)
}

func TestAssetService_Create_DuplicateAsset_ReturnsErrAssetDuplicated(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	repo.SetAsset(&domain.Asset{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Existing Fund",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
		Status:    domain.StatusActive,
	})
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	_, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Duplicate Fund",
		AssetType: domain.AssetTypeFund,
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrAssetDuplicated.Code, errs.As(err).Code)
}

// =====================================================================
// AssetService.Create 正常路径
// =====================================================================

func TestAssetService_Create_Cash_HappyPath(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	got, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "CASH-ALIPAY-CNY",
		Name:      "Alipay Cash",
		AssetType: domain.AssetTypeCash,
		Currency:  "CNY",
	})
	require.NoError(t, err)
	assert.NotZero(t, got.ID)
	assert.Equal(t, domain.AssetTypeCash, got.AssetType)
}

func TestAssetService_Create_DefaultCurrency_CNY(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	got, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
	})
	require.NoError(t, err)
	assert.Equal(t, "CNY", got.Currency)
}

func TestAssetService_Create_DefaultStatus_Active(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	got, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.StatusActive, got.Status)
}

// =====================================================================
// AssetService.Create 事务失败
// =====================================================================

func TestAssetService_Create_TransactionFails_ReturnsError(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	uow := &testutil.MockUoW{FailOnce: true}
	svc := NewAssetService(uow, repo, &mockPlatformRepo{})
	_, err := svc.Create(context.Background(), CreateAssetInput{
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
	})
	require.Error(t, err)
}

// =====================================================================
// AssetService.Update 参数校验
// =====================================================================

func TestAssetService_Update_UserIDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Update(context.Background(), UpdateAssetInput{
		ID:   1,
		Name: "Updated",
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestAssetService_Update_IDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Update(context.Background(), UpdateAssetInput{
		UserID: 1,
		Name:   "Updated",
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestAssetService_Update_AssetNotFound_ReturnsErrAssetNotFound(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	_, err := svc.Update(context.Background(), UpdateAssetInput{
		UserID: 1,
		ID:     999,
		Name:   "Updated",
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrAssetNotFound.Code, errs.As(err).Code)
}

// =====================================================================
// AssetService.Update 正常路径
// =====================================================================

func TestAssetService_Update_HappyPath(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	repo.SetAsset(&domain.Asset{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Old Name",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
		Status:    domain.StatusActive,
	})
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	got, err := svc.Update(context.Background(), UpdateAssetInput{
		UserID: 1,
		ID:      1,
		Name:   "New Name",
		Remark: "Updated remark",
	})
	require.NoError(t, err)
	assert.Equal(t, "New Name", got.Name)
	assert.Equal(t, "Updated remark", got.Remark)
}

// =====================================================================
// AssetService.Delete
// =====================================================================

func TestAssetService_Delete_UserIDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	err := svc.Delete(context.Background(), 0, 1)
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestAssetService_Delete_IDZero_ReturnsInvalidParam(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	err := svc.Delete(context.Background(), 1, 0)
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}

func TestAssetService_Delete_HappyPath(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	repo.SetAsset(&domain.Asset{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
	})
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	err := svc.Delete(context.Background(), 1, 1)
	require.NoError(t, err)
	_, err = repo.GetByID(context.Background(), 1, 1)
	assert.True(t, errors.Is(err, repository.ErrNotFound))
}

// =====================================================================
// AssetService.Get
// =====================================================================

func TestAssetService_Get_NotFound_ReturnsErrAssetNotFound(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, err := svc.Get(context.Background(), 1, 999)
	require.Error(t, err)
	assert.Equal(t, errs.ErrAssetNotFound.Code, errs.As(err).Code)
}

func TestAssetService_Get_HappyPath(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	repo.SetAsset(&domain.Asset{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Test Fund",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	})
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	got, err := svc.Get(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "FUND001", got.AssetCode)
}

// =====================================================================
// AssetService.List
// =====================================================================

func TestAssetService_List_HappyPath(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	repo.SetAsset(&domain.Asset{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "FUND001",
		Name:      "Fund 1",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	})
	repo.SetAsset(&domain.Asset{
		BaseModel:  domain.BaseModel{ID: 2},
		UserID:    1,
		AssetCode: "FUND002",
		Name:      "Fund 2",
		AssetType: domain.AssetTypeFund,
		Currency:  "CNY",
	})
	repo.ListResult = []domain.Asset{
		*repo.ByID[1],
		*repo.ByID[2],
	}
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	list, total, err := svc.List(context.Background(), AssetListInput{
		UserID: 1,
		Page:   1,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, list, 2)
}

// =====================================================================
// AssetService.List with IncludeHoldings
// =====================================================================

func TestAssetService_List_IncludeHoldingsFalse_DoesNotLoadHoldings(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	repo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "STOCK001",
		Name:      "Test Stock",
		AssetType: domain.AssetTypeStock,
		Currency:  "CNY",
	})
	repo.ListResult = []domain.Asset{*repo.ByID[1]}

	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	list, total, err := svc.List(context.Background(), AssetListInput{
		UserID:          1,
		IncludeHoldings: false,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, list, 1)
	assert.Nil(t, list[0].HoldingSummary) // 不应该加载持仓数据
}

func TestAssetService_List_IncludeHoldingsTrue_WithoutHoldingSvc_DoesNotLoad(t *testing.T) {
	repo := testutil.NewMockAssetRepo()
	repo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "STOCK001",
		Name:      "Test Stock",
		AssetType: domain.AssetTypeStock,
		Currency:  "CNY",
	})
	repo.ListResult = []domain.Asset{*repo.ByID[1]}

	// IncludeHoldings=true 但不传递 holdingSvc（为 nil）
	svc := NewAssetService(&testutil.MockUoW{}, repo, &mockPlatformRepo{})
	list, total, err := svc.List(context.Background(), AssetListInput{
		UserID:          1,
		IncludeHoldings: true,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, list, 1)
	assert.Nil(t, list[0].HoldingSummary) // holdingSvc 为 nil，不加载
}

func TestAssetService_List_IncludeHoldingsTrue_WithHoldingSvc_LoadsHoldings(t *testing.T) {
	// 准备 Asset 数据
	assetRepo := testutil.NewMockAssetRepo()
	assetRepo.SetAsset(&domain.Asset{
		BaseModel:  domain.BaseModel{ID: 1},
		UserID:    1,
		AssetCode: "STOCK001",
		Name:      "Test Stock",
		AssetType: domain.AssetTypeStock,
		Currency:  "CNY",
	})
	assetRepo.ListResult = []domain.Asset{*assetRepo.ByID[1]}

	// 准备 Holding 数据
	holdingRepo := testutil.NewMockHoldingRepo()
	holdingRepo.SetHolding(&domain.Holding{
		BaseModel:  domain.BaseModel{ID: 100},
		UserID:    1,
		AssetID:   1,
		PlatformID: 1,
		Quantity:   decimal.NewFromInt(100),
		AvgCost:    decimal.NewFromFloat(10.0),
		TotalCost:  decimal.NewFromInt(1000),
		Status:     domain.HoldingStatusHolding,
	})

	// 创建 HoldingService
	holdingSvc := NewHoldingService(holdingRepo, assetRepo, testutil.NewMockQuoteRepo(), testutil.NewMockRateRepo(), testutil.NewMockPlatformRepo())

	// 创建 AssetService 并传入 holdingSvc
	svc := NewAssetService(&testutil.MockUoW{}, assetRepo, &mockPlatformRepo{}, holdingSvc)
	list, total, err := svc.List(context.Background(), AssetListInput{
		UserID:          1,
		IncludeHoldings: true,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, list, 1)
	assert.NotNil(t, list[0].HoldingSummary) // 应该加载持仓数据
}

func TestAssetService_List_UserIDZero_ReturnsError(t *testing.T) {
	svc := NewAssetService(&testutil.MockUoW{}, testutil.NewMockAssetRepo(), &mockPlatformRepo{})
	_, _, err := svc.List(context.Background(), AssetListInput{
		UserID: 0,
	})
	require.Error(t, err)
	assert.Equal(t, errs.ErrInvalidParam.Code, errs.As(err).Code)
}
