package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eyjian/fin-vault/backend/internal/cache"
	"github.com/eyjian/fin-vault/backend/internal/domain"
	"github.com/eyjian/fin-vault/backend/internal/testutil"
	"github.com/eyjian/fin-vault/backend/pkg/errs"
)

// =====================================================================
// 测试辅助：基于 cache.Provider 的内存 spy（统计调用次数）
// =====================================================================

// memCache 用 sync.Map 实现 cache.Provider，附统计字段。
type memCache struct {
	mu       sync.Mutex
	store    map[string]string
	GetCalls int
	SetCalls int
	DelCalls int
}

func newMemCache() *memCache { return &memCache{store: make(map[string]string)} }

func (c *memCache) Get(_ context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.GetCalls++
	v, ok := c.store[key]
	if !ok {
		return "", cache.ErrCacheMiss
	}
	return v, nil
}

func (c *memCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SetCalls++
	c.store[key] = value
	return nil
}

func (c *memCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DelCalls++
	delete(c.store, key)
	return nil
}

func (c *memCache) Exists(_ context.Context, key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.store[key]
	return ok, nil
}

func (c *memCache) Close() error { return nil }

// =====================================================================
// QuoteService.GetLatest
// =====================================================================

func TestQuoteService_GetLatest_EmptyIDs_ReturnsEmpty(t *testing.T) {
	svc := NewQuoteService(testutil.NewMockQuoteRepo(), testutil.NewMockAssetRepo(), newMemCache(), nil, 0)

	out, err := svc.GetLatest(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestQuoteService_GetLatest_AllMissing_FallsBackToRepoAndCachesResults(t *testing.T) {
	repo := testutil.NewMockQuoteRepo()
	now := time.Now()
	repo.Latest[1] = &domain.PriceQuote{
		AssetID: 1, Price: decimal.RequireFromString("100.50"),
		ChangePct: decimal.RequireFromString("1.23"),
		Volume:    decimal.RequireFromString("10000"),
		QuoteTime: now,
		Source:    domain.QuoteSourceEastmoney,
	}
	repo.Latest[2] = &domain.PriceQuote{
		AssetID: 2, Price: decimal.RequireFromString("3.1415"),
		QuoteTime: now,
		Source:    domain.QuoteSourceManual,
	}
	c := newMemCache()
	svc := NewQuoteService(repo, testutil.NewMockAssetRepo(), c, nil, 30*time.Second)

	out, err := svc.GetLatest(context.Background(), []uint{1, 2, 3 /* 不存在 */})
	require.NoError(t, err)
	assert.Len(t, out, 2, "asset 3 missing should be silently dropped")

	// 命中的 2 条都已写入 cache
	assert.GreaterOrEqual(t, c.SetCalls, 2)

	// 第二次调用：cache 全部命中，repo 应该不被再次访问。
	c.GetCalls = 0
	pre := len(repo.Inserts) // 当前 inserts 长度无关紧要，但确保没多
	out2, err := svc.GetLatest(context.Background(), []uint{1, 2})
	require.NoError(t, err)
	assert.Len(t, out2, 2)
	assert.Equal(t, 2, c.GetCalls, "second call should hit cache, not DB")
	assert.Equal(t, pre, len(repo.Inserts))
}

// =====================================================================
// QuoteService.SaveManual
// =====================================================================

func TestQuoteService_SaveManual_NilOrInvalid(t *testing.T) {
	svc := NewQuoteService(testutil.NewMockQuoteRepo(), testutil.NewMockAssetRepo(), newMemCache(), nil, 0)

	require.Error(t, svc.SaveManual(context.Background(), nil))

	require.Error(t, svc.SaveManual(context.Background(), &domain.PriceQuote{
		AssetID: 0, Price: decimal.RequireFromString("1"),
	}))

	require.Error(t, svc.SaveManual(context.Background(), &domain.PriceQuote{
		AssetID: 1, Price: decimal.Zero,
	}))

	require.Error(t, svc.SaveManual(context.Background(), &domain.PriceQuote{
		AssetID: 1, Price: decimal.RequireFromString("-1"),
	}))
}

func TestQuoteService_SaveManual_DefaultsAndCacheInvalidation(t *testing.T) {
	repo := testutil.NewMockQuoteRepo()
	c := newMemCache()
	// 预置 cache，验证 SaveManual 后会失效
	_ = c.Set(context.Background(), "quote:latest:1", "stale", time.Hour)

	svc := NewQuoteService(repo, testutil.NewMockAssetRepo(), c, nil, 0)

	q := &domain.PriceQuote{
		AssetID: 1,
		Price:   decimal.RequireFromString("12.34"),
		// QuoteTime / Source 留空
	}
	require.NoError(t, svc.SaveManual(context.Background(), q))

	require.Len(t, repo.Inserts, 1)
	saved := repo.Inserts[0]
	assert.Equal(t, domain.QuoteSourceManual, saved.Source, "should default to manual")
	assert.False(t, saved.QuoteTime.IsZero(), "should default to now")
	assert.Equal(t, 1, c.DelCalls, "cache should be invalidated")
}

// =====================================================================
// QuoteService.Refresh：未配置 aggregator 时报错
// =====================================================================

func TestQuoteService_Refresh_NoAggregator_ReturnsErr(t *testing.T) {
	svc := NewQuoteService(testutil.NewMockQuoteRepo(), testutil.NewMockAssetRepo(), newMemCache(), nil, 0)

	_, err := svc.Refresh(context.Background(), 1, []uint{1, 2}, "auto")
	require.Error(t, err)
	be := errs.As(err)
	require.NotNil(t, be)
	assert.Equal(t, errs.ErrQuoteFetchFailed.Code, be.Code)
}
