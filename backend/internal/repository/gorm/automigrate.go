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
		// AI 会话 / 消息 / 步骤（new，基于 trpc-agent-go）
		&domain.Session{},
		&domain.Message{},
		&domain.AgentStep{},
		// AI 把脉结果（一个 (user, asset) 唯一一条最新记录）
		&domain.PulseDiagnosis{},
		// 报表
		&domain.Report{},
		// 系统配置（Tushare Token、DeepSeek API Key 等）
		&domain.SysConfig{},
	)
}
