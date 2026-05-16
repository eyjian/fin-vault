// Package cache 提供进程内 / Redis 双实现的 KV 缓存抽象。
//
// 业务 Service 只 import 本接口，不直接依赖 sync.Map 或 redis 客户端。
// 切换实现：bootstrap 层根据 cfg.Cache.Driver 注入 local 或 redis。
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrCacheMiss 缓存未命中（与 redis.Nil 等价的统一错误）。
var ErrCacheMiss = errors.New("cache: key not found")

// Provider 通用缓存接口。
type Provider interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Close() error
}
