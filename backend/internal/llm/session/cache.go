package session

import (
	"context"
	"time"
)

// Cache AI 会话相关的可选缓存抽象（design.md D4）。
//
// 默认实现 NoopCache 不做任何缓存；后续接 Redis 时新增 RedisCache 实现，业务层无感。
//
// 设计原则：
//   - 缓存 miss 必须返回 (zero, false, nil) 而不是 error，让 service 层用同一条
//     fallback 路径走持久化层；只有真正异常（连接失败等）才返回 error。
//   - TTL=0 表示使用实现的默认值；负值由实现决定是否拒绝。
type Cache interface {
	Get(ctx context.Context, key string) (value []byte, hit bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// NoopCache 不缓存任何内容的 Cache 默认实现，永远返回 miss。
type NoopCache struct{}

// NewNoopCache 构造一个 NoopCache 实例。
func NewNoopCache() *NoopCache { return &NoopCache{} }

// Get 永远返回 miss。
func (NoopCache) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}

// Set 立即丢弃，无副作用。
func (NoopCache) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

// Delete 立即返回 nil，无副作用。
func (NoopCache) Delete(_ context.Context, _ string) error { return nil }

// 编译期断言：NoopCache 必须满足 Cache 接口。
var _ Cache = (*NoopCache)(nil)
