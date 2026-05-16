package platformapi

import (
	"context"
	"errors"
	"sync"

	"github.com/panjf2000/ants/v2"
)

// =====================================================================
// QuoteAggregator —— 多 Provider 聚合 + 协程池并发拉取
// =====================================================================

// QuoteAggregator 行情聚合器。
//
// 行为：
//   - 维护一组 Fetcher（顺序即优先级），按 Supports 路由；
//   - FetchBatch 用 ants 协程池并发处理 N 个资产，超时受 ctx 控制；
//   - 单个资产抓取时按 sourcePriority 顺序尝试，第一个非错就返回。
type QuoteAggregator struct {
	fetchers       map[string]QuoteFetcher
	sourcePriority []string // 来源优先级：["api_eastmoney","api_sina","api_tencent"]
	pool           *ants.Pool
}

// NewAggregator 构造聚合器。poolSize<=0 时默认 16。
func NewAggregator(fetchers []QuoteFetcher, sourcePriority []string, poolSize int) (*QuoteAggregator, error) {
	if poolSize <= 0 {
		poolSize = 16
	}
	pool, err := ants.NewPool(poolSize, ants.WithNonblocking(false))
	if err != nil {
		return nil, err
	}
	m := make(map[string]QuoteFetcher, len(fetchers))
	for _, fch := range fetchers {
		if fch == nil {
			continue
		}
		m[fch.Source()] = fch
	}
	return &QuoteAggregator{
		fetchers:       m,
		sourcePriority: sourcePriority,
		pool:           pool,
	}, nil
}

// Close 释放协程池。
func (a *QuoteAggregator) Close() {
	if a.pool != nil {
		a.pool.Release()
	}
}

// SourcePriority 返回当前源顺序（拷贝，避免外部修改）。
func (a *QuoteAggregator) SourcePriority() []string {
	out := make([]string, len(a.sourcePriority))
	copy(out, a.sourcePriority)
	return out
}

// FetchOne 单条；source 为空时按 sourcePriority 顺序降级，否则只用指定 source。
func (a *QuoteAggregator) FetchOne(ctx context.Context, key AssetKey, source string) (*QuoteResult, error) {
	if source != "" {
		fch, ok := a.fetchers[source]
		if !ok {
			return nil, errors.New("platformapi: source not registered: " + source)
		}
		if !fch.Supports(key) {
			return nil, ErrUnsupportedAsset
		}
		return fch.FetchOne(ctx, key)
	}
	var lastErr error
	for _, src := range a.sourcePriority {
		fch, ok := a.fetchers[src]
		if !ok || !fch.Supports(key) {
			continue
		}
		res, err := fch.FetchOne(ctx, key)
		if err == nil {
			return res, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return nil, ErrUnsupportedAsset
	}
	return nil, lastErr
}

// FetchBatch 并发批量抓取。每条结果一一对应，失败的 Result.Err 被设置（不阻断后续）。
func (a *QuoteAggregator) FetchBatch(ctx context.Context, keys []AssetKey, source string) []QuoteResult {
	results := make([]QuoteResult, len(keys))
	var wg sync.WaitGroup
	wg.Add(len(keys))
	for i, k := range keys {
		i, k := i, k
		_ = a.pool.Submit(func() {
			defer wg.Done()
			r, err := a.FetchOne(ctx, k, source)
			if err != nil {
				results[i] = QuoteResult{AssetID: k.AssetID, Err: err}
				return
			}
			results[i] = *r
		})
	}
	wg.Wait()
	return results
}
