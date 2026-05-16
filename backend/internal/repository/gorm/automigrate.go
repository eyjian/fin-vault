package gormrepo

import (
	"gorm.io/gorm"

	"github.com/eyjian/fin-vault/backend/internal/domain"
)

// AutoMigrate 创建/更新所有 FinVault 表。
//
// 第一阶段使用：开发期表结构频繁变动，AutoMigrate 增量加列；生产环境可关闭。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		// 用户/字典
		&domain.User{},
		&domain.Platform{},
		// 资产核心
		&domain.Asset{},
		&domain.FundDetail{},
		&domain.StockDetail{},
		&domain.WealthDetail{},
		&domain.Holding{},
		&domain.Transaction{},
		&domain.CostLot{},
		&domain.Portfolio{},
		// 行情/汇率
		&domain.PriceQuote{},
		&domain.ExchangeRate{},
		// AI / 报表
		&domain.AIConversation{},
		&domain.AIMessage{},
		&domain.Report{},
	)
}
