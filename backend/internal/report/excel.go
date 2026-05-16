package report

import (
	"context"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// =====================================================================
// ExcelExporter —— excelize/v2 实现
// =====================================================================

type excelExporter struct{}

// NewExcelExporter 构造 Excel 导出器。
func NewExcelExporter() Exporter { return &excelExporter{} }

// Format 返回 xlsx。
func (e *excelExporter) Format() Format { return FormatExcel }

// Export 把 Payload 写入 w。
//
// 多 Sheet 规则：
//   - holdings：Sheet "Holdings"
//   - transactions：Sheet "Transactions"
//   - full：两者都有
func (e *excelExporter) Export(_ context.Context, p Payload, w io.Writer) error {
	f := excelize.NewFile()
	defer f.Close()

	// 删除默认 Sheet1 之前先建好我们的
	if p.Scope == ScopeHoldings || p.Scope == ScopeFull {
		if err := writeHoldingsSheet(f, p.Holdings); err != nil {
			return err
		}
	}
	if p.Scope == ScopeTransactions || p.Scope == ScopeFull {
		if err := writeTransactionsSheet(f, p.Transactions); err != nil {
			return err
		}
	}
	if _, err := f.GetSheetIndex("Sheet1"); err == nil {
		_ = f.DeleteSheet("Sheet1")
	}
	// 设置默认激活的 sheet（防止全删后报错）
	if idx, err := f.GetSheetIndex("Holdings"); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
	} else if idx, err := f.GetSheetIndex("Transactions"); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
	}

	if err := f.Write(w); err != nil {
		return fmt.Errorf("excelize write: %w", err)
	}
	return nil
}

func writeHoldingsSheet(f *excelize.File, rows []HoldingRow) error {
	const sheet = "Holdings"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	headers := []string{
		"ID", "资产代码", "资产名称", "类型", "平台", "币种",
		"持仓数量", "平均成本", "累计成本", "最新价",
		"市值", "浮动盈亏", "已实现盈亏", "累计分红", "总盈亏", "状态", "更新时间",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	for ri, r := range rows {
		row := []any{
			r.ID, r.AssetCode, r.AssetName, r.AssetType, r.PlatformName, r.Currency,
			r.Quantity, r.AvgCost, r.TotalCost, r.LatestPrice,
			r.MarketValue, r.UnrealizedPnL, r.RealizedPnL, r.TotalDividend, r.TotalPnL,
			r.Status, r.UpdatedAt,
		}
		for ci, v := range row {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	return nil
}

func writeTransactionsSheet(f *excelize.File, rows []TransactionRow) error {
	const sheet = "Transactions"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	headers := []string{
		"ID", "交易时间", "类型", "资产代码", "资产名称", "平台ID",
		"数量", "单价", "金额", "手续费", "税费", "净额", "币种", "来源", "备注",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	for ri, r := range rows {
		row := []any{
			r.ID, r.TxnTime, r.TxnType, r.AssetCode, r.AssetName, r.PlatformID,
			r.Quantity, r.Price, r.Amount, r.Fee, r.Tax, r.NetAmount,
			r.Currency, r.Source, r.Note,
		}
		for ci, v := range row {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	return nil
}
