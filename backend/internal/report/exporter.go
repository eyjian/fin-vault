// Package report 提供报表导出抽象与实现。
//
// 抽象 ReportExporter 接口；具体实现：excel.go (excelize/v2) / markdown.go (text/template)。
// service 层只 import 本包，不直接依赖 excelize。
package report

import (
	"context"
	"io"
)

// Format 报表格式。
type Format string

const (
	FormatExcel    Format = "xlsx"
	FormatMarkdown Format = "md"
)

// Scope 报表数据范围。
type Scope string

const (
	ScopeHoldings     Scope = "holdings"
	ScopeTransactions Scope = "transactions"
	ScopeFull         Scope = "full"
)

// HoldingRow 单条持仓导出行（已折算 / 已计算字段）。
type HoldingRow struct {
	ID            uint
	AssetCode     string
	AssetName     string
	AssetType     string
	PlatformName  string
	Currency      string
	Quantity      string
	AvgCost       string
	TotalCost     string
	LatestPrice   string
	MarketValue   string
	UnrealizedPnL string
	RealizedPnL   string
	TotalDividend string
	TotalPnL      string
	Status        string
	UpdatedAt     string
}

// TransactionRow 单条交易导出行。
type TransactionRow struct {
	ID         uint
	TxnTime    string
	TxnType    string
	AssetCode  string
	AssetName  string
	PlatformID uint
	Quantity   string
	Price      string
	Amount     string
	Fee        string
	Tax        string
	NetAmount  string
	Currency   string
	Source     string
	Note       string
}

// Payload 导出数据载荷。
type Payload struct {
	GeneratedAt  string
	Scope        Scope
	Holdings     []HoldingRow
	Transactions []TransactionRow
}

// Exporter 报表写出能力。
type Exporter interface {
	Format() Format
	Export(ctx context.Context, p Payload, w io.Writer) error
}
