package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Asset 资产主表。
type Asset struct {
	BaseModel
	UserID           uint      `gorm:"not null;uniqueIndex:uk_user_code_type,priority:1;index:idx_user_type_status,priority:1;column:f_user_id" json:"user_id"`
	AssetCode        string    `gorm:"size:32;not null;uniqueIndex:uk_user_code_type,priority:2;column:f_asset_code" json:"asset_code"`
	Name             string    `gorm:"size:128;not null;column:f_name" json:"name"`
	AssetType        AssetType `gorm:"size:20;not null;uniqueIndex:uk_user_code_type,priority:3;index:idx_user_type_status,priority:2;column:f_asset_type" json:"asset_type"`
	Currency         string    `gorm:"size:10;not null;default:CNY;column:f_currency" json:"currency"`
	IssuerPlatformID *uint     `gorm:"index:idx_issuer;column:f_issuer_platform_id" json:"issuer_platform_id,omitempty"`
	RiskLevel        string    `gorm:"size:20;column:f_risk_level" json:"risk_level"`
	Status           string    `gorm:"size:20;not null;default:活跃;index:idx_user_type_status,priority:3;column:f_status" json:"status"`
	Remark           string    `gorm:"size:500;column:f_remark" json:"remark"`

	// 关联实体（按需 Preload）
	FundDetail   *FundDetail   `gorm:"foreignKey:AssetID;references:ID" json:"fund_detail,omitempty"`
	StockDetail  *StockDetail  `gorm:"foreignKey:AssetID;references:ID" json:"stock_detail,omitempty"`
	WealthDetail *WealthDetail `gorm:"foreignKey:AssetID;references:ID" json:"wealth_detail,omitempty"`

	// 持仓汇总数据（不入库，仅用于 API 响应）
	HoldingSummary *HoldingSummary `gorm:"-" json:"holding_summary,omitempty"`
}

// HoldingSummary 持仓汇总数据（一个资产可能在多个平台有持仓，此结构存储汇总值）。
type HoldingSummary struct {
	Quantity       decimal.Decimal `json:"quantity"`        // 总持有数量（股数/份额/金额）
	AvgCost        decimal.Decimal `json:"avg_cost"`        // 平均成本
	TotalCost      decimal.Decimal `json:"total_cost"`      // 总成本
	RealizedPnL    decimal.Decimal `json:"realized_pnl"`    // 已实现盈亏
	TotalDividend  decimal.Decimal `json:"total_dividend"`  // 累计分红/利息
	LatestPrice    decimal.Decimal `json:"latest_price"`    // 最新价/净值
	MarketValue    decimal.Decimal `json:"market_value"`    // 市值
	UnrealizedPnL  decimal.Decimal `json:"unrealized_pnl"`  // 未实现盈亏
	TotalPnL       decimal.Decimal `json:"total_pnl"`       // 总盈亏
	PnLRatio       decimal.Decimal `json:"pnl_ratio"`       // 盈亏比率
}

// TableName 显式表名。
func (Asset) TableName() string { return "t_fv_core_assets" }

// FundDetail 基金扩展，1:1 关联 Asset。
type FundDetail struct {
	AssetID       uint            `gorm:"primaryKey;column:f_asset_id" json:"asset_id"`
	FundType      string          `gorm:"size:20;column:f_fund_type" json:"fund_type"`
	Manager       string          `gorm:"size:64;column:f_manager" json:"manager"`
	Company       string          `gorm:"size:128;column:f_company" json:"company"`
	InceptionDate NullableDate    `gorm:"type:date;column:f_inception_date" json:"inception_date,omitempty"`
	LatestNAV     decimal.Decimal `gorm:"type:decimal(20,4);column:f_latest_nav" json:"latest_nav"`
	LatestNAVDate NullableDate    `gorm:"type:date;column:f_latest_nav_date" json:"latest_nav_date,omitempty"`
	Benchmark     string          `gorm:"size:255;column:f_benchmark" json:"benchmark"`
	CreatedAt     time.Time       `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt     time.Time       `gorm:"not null;column:f_updated_at" json:"updated_at"`
}

// TableName 显式表名。
func (FundDetail) TableName() string { return "t_fv_core_fund_details" }

// StockDetail 股票扩展。
type StockDetail struct {
	AssetID         uint            `gorm:"primaryKey;column:f_asset_id" json:"asset_id"`
	Market          string          `gorm:"size:20;not null;index:idx_market;column:f_market" json:"market"`
	Industry        string          `gorm:"size:64;column:f_industry" json:"industry"`
	Sector          string          `gorm:"size:64;column:f_sector" json:"sector"`
	ListingDate     NullableDate    `gorm:"type:date;column:f_listing_date" json:"listing_date,omitempty"`
	TotalShares     decimal.Decimal `gorm:"type:decimal(20,2);column:f_total_shares" json:"total_shares"`
	LatestPrice     decimal.Decimal `gorm:"type:decimal(20,4);column:f_latest_price" json:"latest_price"`
	LatestPriceTime *time.Time      `gorm:"column:f_latest_price_time" json:"latest_price_time,omitempty"`
	CreatedAt       time.Time       `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt       time.Time       `gorm:"not null;column:f_updated_at" json:"updated_at"`
}

// TableName 显式表名。
func (StockDetail) TableName() string { return "t_fv_core_stock_details" }

// WealthDetail 理财产品扩展。
type WealthDetail struct {
	AssetID        uint            `gorm:"primaryKey;column:f_asset_id" json:"asset_id"`
	ProductType    string          `gorm:"size:20;not null;column:f_product_type" json:"product_type"`
	ExpectedYield  decimal.Decimal `gorm:"type:decimal(8,4);column:f_expected_yield" json:"expected_yield"`
	ActualYield    decimal.Decimal `gorm:"type:decimal(8,4);column:f_actual_yield" json:"actual_yield"`
	StartDate      NullableDate    `gorm:"type:date;column:f_start_date" json:"start_date,omitempty"`
	EndDate        NullableDate    `gorm:"type:date;index:idx_end_date;column:f_end_date" json:"end_date,omitempty"`
	TermDays       int             `gorm:"column:f_term_days" json:"term_days"`
	MinAmount      decimal.Decimal `gorm:"type:decimal(20,2);column:f_min_amount" json:"min_amount"`
	RedemptionRule string          `gorm:"size:255;column:f_redemption_rule" json:"redemption_rule"`
	IsAutoRenewal  bool            `gorm:"not null;default:false;column:f_is_auto_renewal" json:"is_auto_renewal"`
	CreatedAt      time.Time       `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt      time.Time       `gorm:"not null;column:f_updated_at" json:"updated_at"`
}

// TableName 显式表名。
func (WealthDetail) TableName() string { return "t_fv_core_wealth_details" }
