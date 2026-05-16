package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Holding 持仓表（业务核心）。
type Holding struct {
	BaseModel
	UserID        uint            `gorm:"not null;uniqueIndex:uk_user_asset_platform,priority:1;index:idx_user_status_platform,priority:1;column:f_user_id" json:"user_id"`
	AssetID       uint            `gorm:"not null;uniqueIndex:uk_user_asset_platform,priority:2;column:f_asset_id" json:"asset_id"`
	PlatformID    uint            `gorm:"not null;uniqueIndex:uk_user_asset_platform,priority:3;index:idx_user_status_platform,priority:3;column:f_platform_id" json:"platform_id"`
	PortfolioID   *uint           `gorm:"index:idx_portfolio;column:f_portfolio_id" json:"portfolio_id,omitempty"`
	Quantity      decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0;column:f_quantity" json:"quantity"`
	AvgCost       decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0;column:f_avg_cost" json:"avg_cost"`
	TotalCost     decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0;column:f_total_cost" json:"total_cost"`
	RealizedPnL   decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0;column:f_realized_pnl" json:"realized_pnl"`
	TotalDividend decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0;column:f_total_dividend" json:"total_dividend"`
	CostMethod    CostMethod      `gorm:"size:20;not null;default:weighted_avg;column:f_cost_method" json:"cost_method"`
	FirstBuyAt    *time.Time      `gorm:"column:f_first_buy_at" json:"first_buy_at,omitempty"`
	LastTxnAt     *time.Time      `gorm:"column:f_last_txn_at" json:"last_txn_at,omitempty"`
	Status        HoldingStatus   `gorm:"size:20;not null;default:holding;index:idx_user_status_platform,priority:2;column:f_status" json:"status"`

	Asset    *Asset    `gorm:"foreignKey:AssetID;references:ID" json:"asset,omitempty"`
	Platform *Platform `gorm:"foreignKey:PlatformID;references:ID" json:"platform,omitempty"`
}

// TableName 显式表名。
func (Holding) TableName() string { return "t_fv_core_holdings" }

// HoldingView 持仓视图，包含实时计算字段，不入库。
type HoldingView struct {
	*Holding
	LatestPrice    decimal.Decimal `json:"latest_price"`
	MarketValue    decimal.Decimal `json:"market_value"`
	UnrealizedPnL  decimal.Decimal `json:"unrealized_pnl"`
	TotalPnL       decimal.Decimal `json:"total_pnl"`
	PnLRatio       decimal.Decimal `json:"pnl_ratio"`
	MarketValueCNY decimal.Decimal `json:"market_value_cny,omitempty"`
}

// CostLot 成本批次（FIFO 辅助）。
type CostLot struct {
	ID           uint            `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	HoldingID    uint            `gorm:"not null;index:idx_holding_status_time,priority:1;column:f_holding_id" json:"holding_id"`
	TxnID        uint            `gorm:"not null;index:idx_txn;column:f_txn_id" json:"txn_id"`
	BuyPrice     decimal.Decimal `gorm:"type:decimal(20,8);not null;column:f_buy_price" json:"buy_price"`
	BuyTime      time.Time       `gorm:"not null;index:idx_holding_status_time,priority:3;column:f_buy_time" json:"buy_time"`
	OriginalQty  decimal.Decimal `gorm:"type:decimal(20,8);not null;column:f_original_qty" json:"original_qty"`
	RemainingQty decimal.Decimal `gorm:"type:decimal(20,8);not null;column:f_remaining_qty" json:"remaining_qty"`
	Status       string          `gorm:"size:20;not null;default:open;index:idx_holding_status_time,priority:2;column:f_status" json:"status"`
	CreatedAt    time.Time       `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt    time.Time       `gorm:"not null;column:f_updated_at" json:"updated_at"`
}

// TableName 显式表名。
func (CostLot) TableName() string { return "t_fv_core_cost_lots" }

// Portfolio 投资组合（一阶段建表 UI 暂不开放）。
type Portfolio struct {
	BaseModel
	UserID           uint   `gorm:"not null;index:idx_user_sort,priority:1;column:f_user_id" json:"user_id"`
	Name             string `gorm:"size:64;not null;column:f_name" json:"name"`
	Description      string `gorm:"size:500;column:f_description" json:"description"`
	TargetAllocation string `gorm:"type:text;column:f_target_allocation" json:"target_allocation"`
	Color            string `gorm:"size:20;column:f_color" json:"color"`
	SortOrder        int    `gorm:"not null;default:0;index:idx_user_sort,priority:2;column:f_sort_order" json:"sort_order"`
}

// TableName 显式表名。
func (Portfolio) TableName() string { return "t_fv_core_portfolios" }
