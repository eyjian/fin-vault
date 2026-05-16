// Package domain 定义 FinVault 的核心领域实体。
//
// 所有实体均使用 GORM tag 直接映射数据库，金额/数量字段统一使用 shopspring/decimal。
// domain 层不实现业务逻辑，仅承载数据结构与领域常量定义。
package domain

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel 提供通用主键 + 软删除时间戳。
type BaseModel struct {
	ID        uint           `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	CreatedAt time.Time      `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;column:f_updated_at" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index;column:f_deleted_at" json:"-"`
}
