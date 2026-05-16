package bootstrap

import (
	"gorm.io/gorm"

	gormrepo "github.com/eyjian/fin-vault/backend/internal/repository/gorm"
)

// Migrate 执行 AutoMigrate，建好 15 张业务表。
func Migrate(db *gorm.DB) error {
	return gormrepo.AutoMigrate(db)
}
