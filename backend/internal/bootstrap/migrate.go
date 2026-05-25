package bootstrap

import (
	"gorm.io/gorm"

	gormrepo "github.com/eyjian/fin-vault/backend/internal/repository/gorm"
)

// Migrate 执行 AutoMigrate，建好 15 张业务表，并迁移枚举值。
func Migrate(db *gorm.DB) error {
	if err := gormrepo.AutoMigrate(db); err != nil {
		return err
	}
	return migrateEnumValues(db)
}

// migrateEnumValues 将数据库中的英文枚举值迁移为中文枚举值。
func migrateEnumValues(db *gorm.DB) error {
	// 迁移用户表状态
	db.Exec("UPDATE t_fv_user_users SET f_status = '活跃' WHERE f_status = 'active'")
	db.Exec("UPDATE t_fv_user_users SET f_status = '禁用' WHERE f_status = 'disabled'")

	// 迁移平台表状态
	db.Exec("UPDATE t_fv_dict_platforms SET f_status = '活跃' WHERE f_status = 'active'")

	// 迁移资产表状态
	db.Exec("UPDATE t_fv_core_assets SET f_status = '活跃' WHERE f_status = 'active'")
	db.Exec("UPDATE t_fv_core_assets SET f_status = '已退市' WHERE f_status = 'delisted'")
	db.Exec("UPDATE t_fv_core_assets SET f_status = '已到期' WHERE f_status = 'matured'")

	// 迁移持仓表状态
	db.Exec("UPDATE t_fv_core_holdings SET f_status = '持有中' WHERE f_status = 'holding'")
	db.Exec("UPDATE t_fv_core_holdings SET f_status = '已关闭' WHERE f_status = 'closed'")
	db.Exec("UPDATE t_fv_core_holdings SET f_status = '已到期' WHERE f_status = 'matured'")

	// 迁移交易表来源
	db.Exec("UPDATE t_fv_txn_transactions SET f_source = '手动' WHERE f_source = 'manual'")
	db.Exec("UPDATE t_fv_txn_transactions SET f_source = '导入' WHERE f_source = 'import'")
	db.Exec("UPDATE t_fv_txn_transactions SET f_source = '自动到期' WHERE f_source = 'auto_mature'")

	// 迁移行情表来源
	db.Exec("UPDATE t_fv_quote_price_quotes SET f_source = '手动' WHERE f_source = 'manual'")
	db.Exec("UPDATE t_fv_quote_price_quotes SET f_source = '东方财富' WHERE f_source = 'api_eastmoney'")
	db.Exec("UPDATE t_fv_quote_price_quotes SET f_source = '新浪' WHERE f_source = 'api_sina'")
	db.Exec("UPDATE t_fv_quote_price_quotes SET f_source = '腾讯' WHERE f_source = 'api_tencent'")

	// 迁移汇率表来源
	db.Exec("UPDATE t_fv_quote_exchange_rates SET f_source = '手动' WHERE f_source = 'manual'")
	db.Exec("UPDATE t_fv_quote_exchange_rates SET f_source = '央行' WHERE f_source = 'pboc'")
	db.Exec("UPDATE t_fv_quote_exchange_rates SET f_source = 'API' WHERE f_source = 'api'")

	return nil
}
