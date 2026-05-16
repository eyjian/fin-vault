package service

import (
	"context"
	"io"
	"time"

	"github.com/shopspring/decimal"

	"github.com/eyjian/fin-vault/backend/internal/report"
	"github.com/eyjian/fin-vault/backend/internal/repository"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// ExportService —— Excel / Markdown 数据导出
// =====================================================================

// ExportService 导出服务。
type ExportService struct {
	holdingRepo  repository.HoldingRepository
	txnRepo      repository.TransactionRepository
	assetRepo    repository.AssetRepository
	platformRepo repository.PlatformRepository
	quoteRepo    repository.QuoteRepository
	exporters    map[report.Format]report.Exporter
}

// NewExportService 构造。
func NewExportService(
	holdingRepo repository.HoldingRepository,
	txnRepo repository.TransactionRepository,
	assetRepo repository.AssetRepository,
	platformRepo repository.PlatformRepository,
	quoteRepo repository.QuoteRepository,
) *ExportService {
	return &ExportService{
		holdingRepo:  holdingRepo,
		txnRepo:      txnRepo,
		assetRepo:    assetRepo,
		platformRepo: platformRepo,
		quoteRepo:    quoteRepo,
		exporters: map[report.Format]report.Exporter{
			report.FormatExcel:    report.NewExcelExporter(),
			report.FormatMarkdown: report.NewMarkdownExporter(),
		},
	}
}

// ExportInput 导出参数。
type ExportInput struct {
	UserID uint
	Format report.Format
	Scope  report.Scope
	Start  time.Time
	End    time.Time
}

// Export 写出文件流到 w。
func (s *ExportService) Export(ctx context.Context, in ExportInput, w io.Writer) error {
	if in.UserID == 0 {
		in.UserID = 1
	}
	if in.Scope == "" {
		in.Scope = report.ScopeHoldings
	}
	if in.Format == "" {
		in.Format = report.FormatExcel
	}
	exp, ok := s.exporters[in.Format]
	if !ok {
		return errs.ErrInvalidParam.WithMsg("unsupported format: " + string(in.Format))
	}

	payload := report.Payload{
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		Scope:       in.Scope,
	}

	if in.Scope == report.ScopeHoldings || in.Scope == report.ScopeFull {
		rows, err := s.collectHoldingRows(ctx, in.UserID)
		if err != nil {
			return err
		}
		payload.Holdings = rows
	}
	if in.Scope == report.ScopeTransactions || in.Scope == report.ScopeFull {
		rows, err := s.collectTransactionRows(ctx, in.UserID, in.Start, in.End)
		if err != nil {
			return err
		}
		payload.Transactions = rows
	}

	return exp.Export(ctx, payload, w)
}

// =====================================================================
// 内部：组装行
// =====================================================================

func (s *ExportService) collectHoldingRows(ctx context.Context, userID uint) ([]report.HoldingRow, error) {
	holdings, _, err := s.holdingRepo.ListByUser(ctx, repository.ListOptions{
		UserID:   userID,
		Page:     1,
		PageSize: 1000,
	})
	if err != nil {
		return nil, err
	}
	if len(holdings) == 0 {
		return nil, nil
	}
	// 平台名映射
	platforms, _ := s.platformRepo.List(ctx)
	platformName := make(map[uint]string, len(platforms))
	for _, p := range platforms {
		platformName[p.ID] = p.Name
	}
	// 行情映射
	assetIDs := make([]uint, 0, len(holdings))
	for _, h := range holdings {
		assetIDs = append(assetIDs, h.AssetID)
	}
	quoteMap, _ := s.quoteRepo.BatchGetLatest(ctx, assetIDs)

	rows := make([]report.HoldingRow, 0, len(holdings))
	for _, h := range holdings {
		asset, _ := s.assetRepo.GetByID(ctx, userID, h.AssetID)
		latest := decimal.Zero
		if q, ok := quoteMap[h.AssetID]; ok && q != nil {
			latest = q.Price
		}
		marketVal := h.Quantity.Mul(latest)
		unreal := marketVal.Sub(h.TotalCost)
		totalPnL := unreal.Add(h.RealizedPnL).Add(h.TotalDividend)
		row := report.HoldingRow{
			ID:            h.ID,
			Quantity:      h.Quantity.String(),
			AvgCost:       h.AvgCost.String(),
			TotalCost:     h.TotalCost.String(),
			LatestPrice:   latest.String(),
			MarketValue:   marketVal.String(),
			UnrealizedPnL: unreal.String(),
			RealizedPnL:   h.RealizedPnL.String(),
			TotalDividend: h.TotalDividend.String(),
			TotalPnL:      totalPnL.String(),
			Status:        string(h.Status),
			UpdatedAt:     h.UpdatedAt.Format("2006-01-02 15:04:05"),
			PlatformName:  platformName[h.PlatformID],
		}
		if asset != nil {
			row.AssetCode = asset.AssetCode
			row.AssetName = asset.Name
			row.AssetType = string(asset.AssetType)
			row.Currency = asset.Currency
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *ExportService) collectTransactionRows(ctx context.Context, userID uint, start, end time.Time) ([]report.TransactionRow, error) {
	filters := map[string]any{}
	if !start.IsZero() {
		filters["start_time"] = start
	}
	if !end.IsZero() {
		filters["end_time"] = end.Add(24 * time.Hour)
	}
	txns, _, err := s.txnRepo.List(ctx, repository.ListOptions{
		UserID:   userID,
		Page:     1,
		PageSize: 5000,
		OrderBy:  "f_txn_time desc",
		Filters:  filters,
	})
	if err != nil {
		return nil, err
	}
	rows := make([]report.TransactionRow, 0, len(txns))
	for _, t := range txns {
		asset, _ := s.assetRepo.GetByID(ctx, userID, t.AssetID)
		row := report.TransactionRow{
			ID:         t.ID,
			TxnTime:    t.TxnTime.Format("2006-01-02 15:04:05"),
			TxnType:    string(t.TxnType),
			PlatformID: t.PlatformID,
			Quantity:   t.Quantity.String(),
			Price:      t.Price.String(),
			Amount:     t.Amount.String(),
			Fee:        t.Fee.String(),
			Tax:        t.Tax.String(),
			NetAmount:  t.NetAmount.String(),
			Currency:   t.Currency,
			Source:     t.Source,
			Note:       t.Note,
		}
		if asset != nil {
			row.AssetCode = asset.AssetCode
			row.AssetName = asset.Name
		}
		rows = append(rows, row)
	}
	return rows, nil
}
