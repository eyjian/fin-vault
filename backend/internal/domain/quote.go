package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// PriceQuote / ExchangeRate 来源常量定义在 enums.go 中（QuoteSource* / RateSource*）。

// PriceQuote 行情快照（基金净值 / 股票价）。
type PriceQuote struct {
	ID        uint            `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	AssetID   uint            `gorm:"not null;index:idx_asset_time,priority:1;column:f_asset_id" json:"asset_id"`
	Price     decimal.Decimal `gorm:"type:decimal(20,8);not null;column:f_price" json:"price"`
	QuoteTime time.Time       `gorm:"not null;index:idx_asset_time,priority:2,sort:desc;column:f_quote_time" json:"quote_time"`
	ChangePct decimal.Decimal `gorm:"type:decimal(10,4);column:f_change_pct" json:"change_pct"`
	Volume    decimal.Decimal `gorm:"type:decimal(20,2);column:f_volume" json:"volume"`
	Source    string          `gorm:"size:20;not null;default:manual;column:f_source" json:"source"`
	CreatedAt time.Time       `gorm:"not null;column:f_created_at" json:"created_at"`
}

// TableName 显式表名。
func (PriceQuote) TableName() string { return "t_fv_quote_price_quotes" }

// ExchangeRate 汇率快照。
type ExchangeRate struct {
	ID           uint            `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	FromCurrency string          `gorm:"size:10;not null;uniqueIndex:uk_from_to_date_source,priority:1;index:idx_from_to_date,priority:1;column:f_from_currency" json:"from_currency"`
	ToCurrency   string          `gorm:"size:10;not null;uniqueIndex:uk_from_to_date_source,priority:2;index:idx_from_to_date,priority:2;column:f_to_currency" json:"to_currency"`
	Rate         decimal.Decimal `gorm:"type:decimal(12,6);not null;column:f_rate" json:"rate"`
	QuoteDate    time.Time       `gorm:"type:date;not null;uniqueIndex:uk_from_to_date_source,priority:3;index:idx_from_to_date,priority:3,sort:desc;column:f_quote_date" json:"quote_date"`
	Source       string          `gorm:"size:20;not null;default:manual;uniqueIndex:uk_from_to_date_source,priority:4;column:f_source" json:"source"`
	CreatedAt    time.Time       `gorm:"not null;column:f_created_at" json:"created_at"`
}

// TableName 显式表名。
func (ExchangeRate) TableName() string { return "t_fv_quote_exchange_rates" }
