package cache

import (
	"context"
	"sync"
	"time"
)

// localItem 单条缓存项。
type localItem struct {
	value     string
	expiresAt time.Time // zero 表示永不过期
}

func (it localItem) expired() bool {
	if it.expiresAt.IsZero() {
		return false
	}
	return time.Now().After(it.expiresAt)
}

// localCache 进程内 KV 缓存（sync.Map + TTL 惰性失效 + 后台清扫）。
type localCache struct {
	store     sync.Map
	stopOnce  sync.Once
	stopCh    chan struct{}
	sweepTick time.Duration
}

// NewLocal 构造本地缓存。sweepInterval 后台扫描间隔；<=0 时默认 1 分钟。
func NewLocal(sweepInterval time.Duration) Provider {
	if sweepInterval <= 0 {
		sweepInterval = time.Minute
	}
	c := &localCache{
		stopCh:    make(chan struct{}),
		sweepTick: sweepInterval,
	}
	go c.sweepLoop()
	return c
}

// Get 取值，未命中返回 ErrCacheMiss。过期视同未命中。
func (c *localCache) Get(_ context.Context, key string) (string, error) {
	v, ok := c.store.Load(key)
	if !ok {
		return "", ErrCacheMiss
	}
	it := v.(localItem)
	if it.expired() {
		c.store.Delete(key)
		return "", ErrCacheMiss
	}
	return it.value, nil
}

// Set 写入；ttl<=0 视为永不过期。
func (c *localCache) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	it := localItem{value: value}
	if ttl > 0 {
		it.expiresAt = time.Now().Add(ttl)
	}
	c.store.Store(key, it)
	return nil
}

// Delete 删除一条。
func (c *localCache) Delete(_ context.Context, key string) error {
	c.store.Delete(key)
	return nil
}

// Exists 判断是否存在且未过期。
func (c *localCache) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.Get(ctx, key)
	if err == nil {
		return true, nil
	}
	if err == ErrCacheMiss {
		return false, nil
	}
	return false, err
}

// Close 停止后台清扫 goroutine。
func (c *localCache) Close() error {
	c.stopOnce.Do(func() { close(c.stopCh) })
	return nil
}

func (c *localCache) sweepLoop() {
	t := time.NewTicker(c.sweepTick)
	defer t.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-t.C:
			c.store.Range(func(k, v any) bool {
				if v.(localItem).expired() {
					c.store.Delete(k)
				}
				return true
			})
		}
	}
}
