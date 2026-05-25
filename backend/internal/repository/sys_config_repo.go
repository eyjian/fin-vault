package repository

import "context"

// SysConfigRepository 系统配置仓储（键值对存储，用于 Tushare/DeepSeek 等配置）。
type SysConfigRepository interface {
	// GetByCategory 按 category 获取全部配置项。
	GetByCategory(ctx context.Context, category string) ([]SysConfigEntry, error)

	// Get 按 category + key 获取单个配置项。不存在返回 (nil, nil)。
	Get(ctx context.Context, category, key string) (*SysConfigEntry, error)

	// Upsert 创建或更新配置项（命中 category+key 时覆盖 value/remark）。
	Upsert(ctx context.Context, entry *SysConfigEntry) error

	// Delete 删除配置项。
	Delete(ctx context.Context, category, key string) error

	// GetAll 获取全部配置项。
	GetAll(ctx context.Context) ([]SysConfigEntry, error)
}

// SysConfigEntry 系统配置项（供 service/handler 层使用的 DTO）。
type SysConfigEntry struct {
	ID       uint   `json:"id"`
	Category string `json:"category"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	Remark   string `json:"remark,omitempty"`
}
