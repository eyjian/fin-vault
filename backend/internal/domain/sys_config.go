package domain

import "time"

// SysConfig 系统配置表（键值对存储，支持分类）。
//
// 用于存储 Tushare Token、DeepSeek API Key 等配置项，
// 便于通过前端设置页动态管理，无需修改配置文件。
//
// 唯一约束 (category, key)：同一分类下的键名唯一。
type SysConfig struct {
	ID        uint      `gorm:"primaryKey;autoIncrement;column:f_id" json:"id"`
	Category  string    `gorm:"size:64;not null;uniqueIndex:uk_category_key,priority:1;column:f_category" json:"category"` // 分类：tushare / deepseek / llm / ...
	Key       string    `gorm:"size:128;not null;uniqueIndex:uk_category_key,priority:2;column:f_key" json:"key"`          // 键名：token / base_url / enabled / ...
	Value     string    `gorm:"type:text;column:f_value" json:"value"`                                                     // 值
	Remark    string    `gorm:"size:500;column:f_remark" json:"remark,omitempty"`                                          // 备注（如配置说明）
	CreatedAt time.Time `gorm:"not null;column:f_created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;column:f_updated_at" json:"updated_at"`
}

// TableName 显式表名。
func (SysConfig) TableName() string { return "t_fv_sys_configs" }

// SysConfigCategory 定义系统配置分类常量。
const (
	SysConfigCategoryTushare  = "tushare"
	SysConfigCategoryDeepSeek = "deepseek"
	SysConfigCategoryLLM      = "llm"
)
