package bootstrap

import (
	"time"

	"github.com/eyjian/fin-vault/backend/internal/cache"
)

// NewCache 根据 CacheConfig 构造缓存 Provider。
//
// 第一阶段只实现 local（sync.Map + TTL）；接 redis 时新增 case。
func NewCache(cfg CacheConfig) cache.Provider {
	switch cfg.Driver {
	case "redis":
		// TODO(M2): redis 实现，需要 internal/cache/redis.go
		// 目前 fallback 到 local，避免阻塞 M1 启动
		return cache.NewLocal(time.Minute)
	case "memory", "local", "":
		return cache.NewLocal(time.Minute)
	default:
		return cache.NewLocal(time.Minute)
	}
}
