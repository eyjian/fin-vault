package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/report"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// 测试夹具：构造一个完整的 ExportService + 注入数据
// =====================================================================

// exportFixture 测试夹具，提供常用 mock 与构造好的 service。
type exportFixture struct {
	holdingRepo  *testutil.MockHoldingRepo
	txnRepo      *testutil.MockTransactionRepo
	assetRepo    *testutil.MockAssetRepo
	platformRepo *fakePlatformRepo
	quoteRepo    *testutil.MockQuoteRepo
	svc          *ExportService
}

func newExportFixture() *exportFixture {
	hr := testutil.NewMockHoldingRepo()
	tr := testutil.NewMockTransactionRepo()
	ar := testutil.NewMockAssetRepo()
	pr := newFakePlatformRepo()
	qr := testutil.NewMockQuoteRepo()
	return &exportFixture{
		holdingRepo:  hr,
		txnRepo:      tr,
		assetRepo:    ar,
		platformRepo: pr,
		quoteRepo:    qr,
		svc:          NewExportService(hr, tr, ar, pr, qr),
	}
}

// fakePlatformRepo 实现 PlatformRepository（testutil 没有 mock，本地手写）。
type fakePlatformRepo struct {
	platforms map[uint]*domain.Platform
}

func newFakePlatformRepo() *fakePlatformRepo {
	return &fakePlatformRepo{platforms: make(map[uint]*domain.Platform)}
}

func (p *fakePlatformRepo) Set(plat *domain.Platform) { p.platforms[plat.ID] = plat }

func (p *fakePlatformRepo) List(_ context.Context) ([]domain.Platform, error) {
	out := make([]domain.Platform, 0, len(p.platforms))
	for _, plat := range p.platforms {
		out = append(out, *plat)
	}
	return out, nil
}
func (p *fakePlatformRepo) GetByID(_ context.Context, id uint) (*domain.Platform, error) {
	if plat, ok := p.platforms[id]; ok {
		return plat, nil
	}
	return nil, errors.New("not found")
}
func (p *fakePlatformRepo) GetByCode(_ context.Context, _ string) (*domain.Platform, error) {
	return nil, errors.New("not found")
}
func (p *fakePlatformRepo) Create(_ context.Context, plat *domain.Platform) error {
	if plat.ID == 0 {
		plat.ID = uint(len(p.platforms) + 1)
	}
	p.platforms[plat.ID] = plat
	return nil
}
func (p *fakePlatformRepo) Update(_ context.Context, plat *domain.Platform) error {
	p.platforms[plat.ID] = plat
	return nil
}
func (p *fakePlatformRepo) Delete(_ context.Context, id uint) error {
	delete(p.platforms, id)
	return nil
}

// =====================================================================
// 1. Format / Scope 默认值
// =====================================================================

func TestExportService_Export_DefaultFormatAndScope(t *testing.T) {
	fx := newExportFixture()
	// holdings 列表为空，但调用应仍成功
	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{}, &buf)
	require.NoError(t, err)

	// 默认 format=xlsx → 输出非空（xlsx 即使空也有 zip header）
	assert.Greater(t, buf.Len(), 0, "default xlsx should produce output")

	// 验证产物确实是 xlsx
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err, "default format should be xlsx")
	defer f.Close()

	// 默认 scope=holdings → 应该有 Holdings sheet
	idx, err := f.GetSheetIndex("Holdings")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, idx, 0)
}

// =====================================================================
// 2. 不支持的 format → ErrInvalidParam
// =====================================================================

func TestExportService_Export_UnsupportedFormat_ReturnsInvalidParam(t *testing.T) {
	fx := newExportFixture()
	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1,
		Format: report.Format("pdf"),
		Scope:  report.ScopeHoldings,
	}, &buf)
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrInvalidParam.Code, be.Code)
	assert.Contains(t, be.Message, "pdf")
	assert.Equal(t, 0, buf.Len(), "writer should not have been written to")
}

// =====================================================================
// 3. Scope=holdings + Markdown：完整表格 + 平台名 + 行情命中/未命中
// =====================================================================

func TestExportService_Export_MarkdownHoldings_FullTable(t *testing.T) {
	fx := newExportFixture()

	// 注入 2 个 holding（id=1 命中行情，id=2 未命中）
	fx.holdingRepo.SetHolding(&domain.Holding{
		BaseModel:     domain.BaseModel{ID: 1},
		UserID:        1, AssetID: 100, PlatformID: 10,
		Quantity:      decimal.RequireFromString("10"),
		AvgCost:       decimal.RequireFromString("100"),
		TotalCost:     decimal.RequireFromString("1000"),
		RealizedPnL:   decimal.RequireFromString("50"),
		TotalDividend: decimal.RequireFromString("20"),
		Status:        domain.HoldingStatusHolding,
	})
	fx.holdingRepo.SetHolding(&domain.Holding{
		BaseModel: domain.BaseModel{ID: 2},
		UserID:    1, AssetID: 200, PlatformID: 11,
		Quantity:  decimal.RequireFromString("5"),
		AvgCost:   decimal.RequireFromString("200"),
		TotalCost: decimal.RequireFromString("1000"),
		Status:    domain.HoldingStatusHolding,
	})

	fx.assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    1, AssetCode: "110022", Name: "易方达消费",
		AssetType: domain.AssetTypeFund, Currency: "CNY",
	})
	fx.assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 200},
		UserID:    1, AssetCode: "00700", Name: "腾讯控股",
		AssetType: domain.AssetTypeStock, Currency: "HKD",
	})

	fx.platformRepo.Set(&domain.Platform{ID: 10, Name: "天天基金"})
	fx.platformRepo.Set(&domain.Platform{ID: 11, Name: "富途牛牛"})

	// 仅 assetID=100 有最新行情
	fx.quoteRepo.Latest[100] = &domain.PriceQuote{
		AssetID: 100, Price: decimal.RequireFromString("120"),
	}

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1,
		Format: report.FormatMarkdown,
		Scope:  report.ScopeHoldings,
	}, &buf)
	require.NoError(t, err)

	out := buf.String()

	// 标题与表头
	assert.Contains(t, out, "## 持仓 (Holdings)")
	assert.Contains(t, out, "代码 | 名称")

	// 第 1 条（命中行情）：market_value = 10 * 120 = 1200, unreal = 200, total_pnl = 200+50+20 = 270
	assert.Contains(t, out, "110022")
	assert.Contains(t, out, "易方达消费")
	assert.Contains(t, out, "天天基金")
	assert.Contains(t, out, "| 1200 |", "market_value should be 1200")
	assert.Contains(t, out, "| 200 |", "unrealized_pnl should be 200")
	assert.Contains(t, out, "| 270 |", "total_pnl should be 270")

	// 第 2 条（未命中行情）：latest_price = "0", market_value = 0
	assert.Contains(t, out, "00700")
	assert.Contains(t, out, "腾讯控股")
	assert.Contains(t, out, "富途牛牛")

	// 顺序：第 1 条在第 2 条之前
	idx1 := strings.Index(out, "110022")
	idx2 := strings.Index(out, "00700")
	require.Greater(t, idx1, 0)
	require.Greater(t, idx2, 0)
	assert.Less(t, idx1, idx2, "holdings should be sorted by ID asc")
}

// =====================================================================
// 4. Scope=transactions：start/end 走 filters；OrderBy=f_txn_time desc
// =====================================================================
//
// 注：MockTransactionRepo.List 当前忽略 OrderBy/Filters（仅返回全量 Inserts），
// 严格断言 OrderBy/filters 需要装饰 mock。Go 接口分发不允许通过嵌入字段覆盖
// 父类型方法被 interface 调用，所以这里用「输出特征」间接验证：
//   - 通过断言 markdown 中含交易记录、不含持仓节，验证 Scope=transactions 路由正确
//   - OrderBy 与 Filters 的实际生效将在 D1 gormimpl 上线后由 sqlmock 测试覆盖

func TestExportService_Export_TransactionsScope_HappyPath(t *testing.T) {
	fx := newExportFixture()

	// 注入 2 笔交易，属于不同资产
	fx.assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    1, AssetCode: "110022", Name: "易方达",
	})
	t1 := time.Date(2026, 5, 10, 9, 0, 0, 0, time.Local)
	t2 := time.Date(2026, 5, 14, 14, 30, 0, 0, time.Local)
	_ = fx.txnRepo.Create(context.Background(), &domain.Transaction{
		UserID: 1, AssetID: 100, PlatformID: 10, HoldingID: 1,
		TxnType: domain.TxnTypeBuy, TxnTime: t1,
		Quantity: decimal.RequireFromString("100"),
		Price:    decimal.RequireFromString("2.50"),
		Amount:   decimal.RequireFromString("250"),
		Fee:      decimal.RequireFromString("0.50"),
		NetAmount: decimal.RequireFromString("250.50"),
		Currency:  "CNY", Source: domain.TxnSourceManual,
	})
	_ = fx.txnRepo.Create(context.Background(), &domain.Transaction{
		UserID: 1, AssetID: 100, PlatformID: 10, HoldingID: 1,
		TxnType: domain.TxnTypeSell, TxnTime: t2,
		Quantity: decimal.RequireFromString("50"),
		Price:    decimal.RequireFromString("3.00"),
		Amount:   decimal.RequireFromString("150"),
		NetAmount: decimal.RequireFromString("149.70"),
		Currency:  "CNY", Source: domain.TxnSourceManual,
		Note: "卖出止盈",
	})

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1,
		Format: report.FormatMarkdown,
		Scope:  report.ScopeTransactions,
		Start:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local),
		End:    time.Date(2026, 5, 31, 0, 0, 0, 0, time.Local),
	}, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "## 交易流水 (Transactions)")
	assert.Contains(t, out, "buy")
	assert.Contains(t, out, "sell")
	assert.Contains(t, out, "卖出止盈")
	assert.Contains(t, out, "110022")
	// holdings 区域不应出现
	assert.NotContains(t, out, "## 持仓")
}

// =====================================================================
// 5. Scope=full + xlsx：双 sheet + headers
// =====================================================================

func TestExportService_Export_FullScope_XlsxHasBothSheets(t *testing.T) {
	fx := newExportFixture()

	fx.holdingRepo.SetHolding(&domain.Holding{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1, AssetID: 100, PlatformID: 10,
		Quantity:  decimal.RequireFromString("10"),
		AvgCost:   decimal.RequireFromString("100"),
		TotalCost: decimal.RequireFromString("1000"),
		Status:    domain.HoldingStatusHolding,
	})
	fx.assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    1, AssetCode: "110022", Name: "易方达",
		AssetType: domain.AssetTypeFund,
	})
	_ = fx.txnRepo.Create(context.Background(), &domain.Transaction{
		UserID: 1, AssetID: 100, PlatformID: 10, HoldingID: 1,
		TxnType:  domain.TxnTypeBuy,
		TxnTime:  time.Now(),
		Quantity: decimal.RequireFromString("100"),
		Price:    decimal.RequireFromString("2.50"),
		Amount:   decimal.RequireFromString("250"),
		NetAmount: decimal.RequireFromString("250.50"),
		Currency:  "CNY", Source: domain.TxnSourceManual,
	})

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1,
		Format: report.FormatExcel,
		Scope:  report.ScopeFull,
	}, &buf)
	require.NoError(t, err)
	require.Greater(t, buf.Len(), 0)

	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	defer f.Close()

	// 两个 sheet 都存在
	hIdx, err := f.GetSheetIndex("Holdings")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, hIdx, 0)
	tIdx, err := f.GetSheetIndex("Transactions")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, tIdx, 0)

	// Holdings A1 = "ID"，B2 = "110022"
	val, err := f.GetCellValue("Holdings", "A1")
	require.NoError(t, err)
	assert.Equal(t, "ID", val)
	val, err = f.GetCellValue("Holdings", "B2")
	require.NoError(t, err)
	assert.Equal(t, "110022", val)

	// Transactions A1 = "ID"，C2 = "buy"
	val, err = f.GetCellValue("Transactions", "A1")
	require.NoError(t, err)
	assert.Equal(t, "ID", val)
	val, err = f.GetCellValue("Transactions", "C2")
	require.NoError(t, err)
	assert.Equal(t, "buy", val)

	// 默认 Sheet1 已删除
	if idx, err := f.GetSheetIndex("Sheet1"); err == nil {
		assert.Less(t, idx, 0, "Sheet1 should be deleted")
	}
}

// =====================================================================
// 6. 空数据
// =====================================================================

func TestExportService_Export_EmptyHoldings_MarkdownPlaceholder(t *testing.T) {
	fx := newExportFixture()

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatMarkdown, Scope: report.ScopeHoldings,
	}, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "_暂无持仓_")
}

func TestExportService_Export_EmptyHoldings_XlsxStillValid(t *testing.T) {
	fx := newExportFixture()

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatExcel, Scope: report.ScopeHoldings,
	}, &buf)
	require.NoError(t, err)
	assert.Greater(t, buf.Len(), 0)

	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	defer f.Close()
	// Holdings sheet 仍存在，仅有 header 行
	val, err := f.GetCellValue("Holdings", "A1")
	require.NoError(t, err)
	assert.Equal(t, "ID", val)
}

// =====================================================================
// 7. 持仓计算正确性（D2 重点强调）
// =====================================================================

func TestExportService_HoldingCalculations_MarketValueAndPnL(t *testing.T) {
	fx := newExportFixture()
	fx.holdingRepo.SetHolding(&domain.Holding{
		BaseModel:     domain.BaseModel{ID: 1},
		UserID:        1, AssetID: 100, PlatformID: 10,
		Quantity:      decimal.RequireFromString("10"),
		TotalCost:     decimal.RequireFromString("1000"),
		AvgCost:       decimal.RequireFromString("100"),
		RealizedPnL:   decimal.RequireFromString("50"),
		TotalDividend: decimal.RequireFromString("20"),
		Status:        domain.HoldingStatusHolding,
	})
	fx.assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    1, AssetCode: "TEST", Name: "TestAsset",
	})
	fx.quoteRepo.Latest[100] = &domain.PriceQuote{
		AssetID: 100, Price: decimal.RequireFromString("120"),
	}

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatMarkdown, Scope: report.ScopeHoldings,
	}, &buf)
	require.NoError(t, err)

	out := buf.String()
	// market_value = 10 * 120 = 1200
	// unrealized_pnl = 1200 - 1000 = 200
	// total_pnl = 200 + 50 + 20 = 270
	assert.Contains(t, out, "| 1200 |", "market_value should be 1200")
	assert.Contains(t, out, "| 200 |", "unrealized_pnl should be 200")
	assert.Contains(t, out, "| 270 |", "total_pnl should be 270")
}

// =====================================================================
// 8. 错误传递：holdingRepo.ListByUser 报错 → exporter 不被调用
// =====================================================================

func TestExportService_Export_HoldingListError_PropagatesAndSkipsExporter(t *testing.T) {
	fx := newExportFixture()
	fx.holdingRepo.ListByUserErr = errors.New("db down: holdings")

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatMarkdown, Scope: report.ScopeHoldings,
	}, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down: holdings")
	// exporter 未被调用 → buffer 应为空
	assert.Equal(t, 0, buf.Len(), "exporter should not run when collect fails")
}

func TestExportService_Export_FullScope_HoldingListError_StopsBeforeTxn(t *testing.T) {
	fx := newExportFixture()
	fx.holdingRepo.ListByUserErr = errors.New("db down")
	// 即使有交易数据，也不应被处理（holdings 段先报错）
	_ = fx.txnRepo.Create(context.Background(), &domain.Transaction{
		UserID: 1, AssetID: 100, PlatformID: 10, HoldingID: 1,
		TxnType: domain.TxnTypeBuy, TxnTime: time.Now(),
		Quantity: decimal.RequireFromString("1"),
		Price:    decimal.RequireFromString("1"),
		Amount:   decimal.RequireFromString("1"),
		NetAmount: decimal.RequireFromString("1"),
		Currency:  "CNY", Source: domain.TxnSourceManual,
	})

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatExcel, Scope: report.ScopeFull,
	}, &buf)
	require.Error(t, err)
	assert.Equal(t, 0, buf.Len())
}

// =====================================================================
// 9. Markdown 转义：asset.Name 含 "|" → 输出 "\|"
// =====================================================================

func TestExportService_MarkdownEscape_PipeInName(t *testing.T) {
	fx := newExportFixture()

	fx.holdingRepo.SetHolding(&domain.Holding{
		BaseModel: domain.BaseModel{ID: 1},
		UserID:    1, AssetID: 100, PlatformID: 10,
		Quantity:  decimal.RequireFromString("1"),
		TotalCost: decimal.RequireFromString("100"),
		Status:    domain.HoldingStatusHolding,
	})
	fx.assetRepo.SetAsset(&domain.Asset{
		BaseModel: domain.BaseModel{ID: 100},
		UserID:    1,
		AssetCode: "TEST",
		Name:      "A|B|C", // 含管道符
	})
	fx.platformRepo.Set(&domain.Platform{ID: 10, Name: "PlatA|PlatB"})

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatMarkdown, Scope: report.ScopeHoldings,
	}, &buf)
	require.NoError(t, err)
	out := buf.String()

	// Name 中的 | 应被转义为 \|
	assert.Contains(t, out, `A\|B\|C`)
	assert.Contains(t, out, `PlatA\|PlatB`)
	// 不应出现裸的 "A|B|C"（会破坏 Markdown 表格）
	assert.NotContains(t, out, "| A|B|C |")
}

func TestExportService_MarkdownEscape_NewlineInNote(t *testing.T) {
	fx := newExportFixture()
	_ = fx.txnRepo.Create(context.Background(), &domain.Transaction{
		UserID: 1, AssetID: 100, PlatformID: 10, HoldingID: 1,
		TxnType: domain.TxnTypeBuy,
		TxnTime: time.Now(),
		Quantity: decimal.RequireFromString("1"),
		Price:    decimal.RequireFromString("1"),
		Amount:   decimal.RequireFromString("1"),
		NetAmount: decimal.RequireFromString("1"),
		Currency:  "CNY", Source: domain.TxnSourceManual,
		Note: "line1\nline2",
	})

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatMarkdown, Scope: report.ScopeTransactions,
	}, &buf)
	require.NoError(t, err)
	out := buf.String()
	// 换行被替换为空格
	assert.Contains(t, out, "line1 line2")
	assert.NotContains(t, out, "line1\nline2")
}

// =====================================================================
// 10. End 日期被 +1 day 处理验证（间接：传 End 后 happy path 仍正常）
// =====================================================================

func TestExportService_Export_TransactionsWithDateRange(t *testing.T) {
	fx := newExportFixture()
	_ = fx.txnRepo.Create(context.Background(), &domain.Transaction{
		UserID: 1, AssetID: 100, PlatformID: 10, HoldingID: 1,
		TxnType: domain.TxnTypeBuy,
		TxnTime: time.Date(2026, 5, 15, 10, 0, 0, 0, time.Local),
		Quantity: decimal.RequireFromString("1"),
		Price:    decimal.RequireFromString("1"),
		Amount:   decimal.RequireFromString("1"),
		NetAmount: decimal.RequireFromString("1"),
		Currency:  "CNY", Source: domain.TxnSourceManual,
	})

	var buf bytes.Buffer
	err := fx.svc.Export(context.Background(), ExportInput{
		UserID: 1, Format: report.FormatMarkdown, Scope: report.ScopeTransactions,
		Start: time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local),
		End:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.Local),
	}, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "buy")
}
