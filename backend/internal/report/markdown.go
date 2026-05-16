package report

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// =====================================================================
// MarkdownExporter —— 标准库 strings.Builder 实现
// =====================================================================

type markdownExporter struct{}

// NewMarkdownExporter 构造 Markdown 导出器。
func NewMarkdownExporter() Exporter { return &markdownExporter{} }

// Format 返回 md。
func (m *markdownExporter) Format() Format { return FormatMarkdown }

// Export 写出 Markdown。
func (m *markdownExporter) Export(_ context.Context, p Payload, w io.Writer) error {
	var b strings.Builder
	b.WriteString("# FinVault 数据导出\n\n")
	if p.GeneratedAt != "" {
		fmt.Fprintf(&b, "> 生成时间：%s\n\n", p.GeneratedAt)
	}
	if p.Scope == ScopeHoldings || p.Scope == ScopeFull {
		writeHoldingsMD(&b, p.Holdings)
	}
	if p.Scope == ScopeTransactions || p.Scope == ScopeFull {
		writeTransactionsMD(&b, p.Transactions)
	}
	if _, err := w.Write([]byte(b.String())); err != nil {
		return fmt.Errorf("md write: %w", err)
	}
	return nil
}

func writeHoldingsMD(b *strings.Builder, rows []HoldingRow) {
	b.WriteString("## 持仓 (Holdings)\n\n")
	if len(rows) == 0 {
		b.WriteString("_暂无持仓_\n\n")
		return
	}
	b.WriteString("| ID | 代码 | 名称 | 类型 | 平台 | 币种 | 数量 | 均价 | 累计成本 | 最新价 | 市值 | 浮动盈亏 | 已实现 | 分红 | 总盈亏 | 状态 |\n")
	b.WriteString("|----|------|------|------|------|------|------|------|----------|--------|------|----------|--------|------|--------|------|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %d | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			r.ID, mdEsc(r.AssetCode), mdEsc(r.AssetName), r.AssetType, mdEsc(r.PlatformName), r.Currency,
			r.Quantity, r.AvgCost, r.TotalCost, r.LatestPrice,
			r.MarketValue, r.UnrealizedPnL, r.RealizedPnL, r.TotalDividend, r.TotalPnL, r.Status,
		)
	}
	b.WriteString("\n")
}

func writeTransactionsMD(b *strings.Builder, rows []TransactionRow) {
	b.WriteString("## 交易流水 (Transactions)\n\n")
	if len(rows) == 0 {
		b.WriteString("_暂无交易_\n\n")
		return
	}
	b.WriteString("| ID | 时间 | 类型 | 代码 | 名称 | 平台 | 数量 | 单价 | 金额 | 净额 | 币种 | 来源 | 备注 |\n")
	b.WriteString("|----|------|------|------|------|------|------|------|------|------|------|------|------|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %d | %s | %s | %s | %s | %d | %s | %s | %s | %s | %s | %s | %s |\n",
			r.ID, r.TxnTime, r.TxnType, mdEsc(r.AssetCode), mdEsc(r.AssetName), r.PlatformID,
			r.Quantity, r.Price, r.Amount, r.NetAmount, r.Currency, r.Source, mdEsc(r.Note),
		)
	}
	b.WriteString("\n")
}

func mdEsc(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
