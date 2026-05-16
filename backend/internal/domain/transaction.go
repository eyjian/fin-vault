package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Transaction 交易流水（事件流）。
//
// 关于外部订单号防重：schema 设计上是 (user_id, platform_id, external_id) WHERE external_id IS NOT NULL 的部分唯一索引，
// 但 GORM AutoMigrate 不支持部分索引声明，因此这里只声明普通组合索引，
// 防重逻辑由 Service 层通过 TransactionRepository.ExistsByExternalID 保证。
type Transaction struct {
	BaseModel
	UserID     uint            `gorm:"not null;index:idx_user_holding_time,priority:1;index:idx_user_time,priority:1;index:idx_user_asset_time,priority:1;index:idx_user_platform_time,priority:1;index:idx_user_platform_external,priority:1;column:f_user_id" json:"user_id"`
	HoldingID  uint            `gorm:"not null;index:idx_user_holding_time,priority:2;column:f_holding_id" json:"holding_id"`
	AssetID    uint            `gorm:"not null;index:idx_user_asset_time,priority:2;column:f_asset_id" json:"asset_id"`
	PlatformID uint            `gorm:"not null;index:idx_user_platform_time,priority:2;index:idx_user_platform_external,priority:2;column:f_platform_id" json:"platform_id"`
	TxnType    TxnType         `gorm:"size:20;not null;column:f_txn_type" json:"txn_type"`
	TxnTime    time.Time       `gorm:"not null;index:idx_user_holding_time,priority:3;index:idx_user_time,priority:2;index:idx_user_asset_time,priority:3;index:idx_user_platform_time,priority:3;column:f_txn_time" json:"txn_time"`
	Quantity   decimal.Decimal `gorm:"type:decimal(20,8);not null;column:f_quantity" json:"quantity"`
	Price      decimal.Decimal `gorm:"type:decimal(20,8);not null;column:f_price" json:"price"`
	Amount     decimal.Decimal `gorm:"type:decimal(20,2);not null;column:f_amount" json:"amount"`
	Fee        decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0;column:f_fee" json:"fee"`
	Tax        decimal.Decimal `gorm:"type:decimal(20,2);not null;default:0;column:f_tax" json:"tax"`
	NetAmount  decimal.Decimal `gorm:"type:decimal(20,2);not null;column:f_net_amount" json:"net_amount"`
	Currency   string          `gorm:"size:10;not null;default:CNY;column:f_currency" json:"currency"`
	Source     string          `gorm:"size:20;not null;default:manual;column:f_source" json:"source"`
	ExternalID string          `gorm:"size:64;index:idx_user_platform_external,priority:3;column:f_external_id" json:"external_id"`
	Note       string          `gorm:"size:500;column:f_note" json:"note"`

	Holding *Holding `gorm:"foreignKey:HoldingID;references:ID" json:"holding,omitempty"`
	Asset   *Asset   `gorm:"foreignKey:AssetID;references:ID" json:"asset,omitempty"`
}

// TableName 显式表名。
func (Transaction) TableName() string { return "t_fv_core_transactions" }
