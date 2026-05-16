package platformapi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// extractJSONString / extractJSONNumber / truncate（util.go 纯函数）
// =====================================================================

func TestExtractJSONString_HappyPath(t *testing.T) {
	body := `jsonpgz({"fundcode":"110022","name":"易方达","gsz":"2.6512","gszzl":"-0.32","gztime":"2026-05-15 15:00"});`
	assert.Equal(t, "110022", extractJSONString(body, "fundcode"))
	assert.Equal(t, "2.6512", extractJSONString(body, "gsz"))
	assert.Equal(t, "-0.32", extractJSONString(body, "gszzl"))
	assert.Equal(t, "2026-05-15 15:00", extractJSONString(body, "gztime"))
}

func TestExtractJSONString_MissingKey_ReturnsEmpty(t *testing.T) {
	body := `{"a":"1"}`
	assert.Equal(t, "", extractJSONString(body, "b"))
}

func TestExtractJSONNumber_HappyPath(t *testing.T) {
	body := `{"data":{"f43":172800,"f170":-15,"f57":"600519"}}`
	assert.Equal(t, "172800", extractJSONNumber(body, "f43"))
	assert.Equal(t, "-15", extractJSONNumber(body, "f170"))
}

func TestExtractJSONNumber_MissingKey_ReturnsEmpty(t *testing.T) {
	body := `{"x":1}`
	assert.Equal(t, "", extractJSONNumber(body, "y"))
}

func TestTruncate_ShortAndLong(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 10))
	assert.Equal(t, "abcde...", truncate("abcdefghij", 5))
}

// =====================================================================
// Fetcher.Supports 路由
// =====================================================================

func TestEastmoneyFetcher_Supports(t *testing.T) {
	f := NewEastmoneyFetcher(0)
	assert.True(t, f.Supports(AssetKey{AssetType: "fund"}))
	assert.True(t, f.Supports(AssetKey{AssetType: "stock", Market: "SH"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "wealth"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "cash"}))
}

func TestSinaFetcher_Supports(t *testing.T) {
	f := NewSinaFetcher(0)
	assert.True(t, f.Supports(AssetKey{AssetType: "stock", Market: "SH"}))
	assert.True(t, f.Supports(AssetKey{AssetType: "stock", Market: "SZ"}))
	assert.True(t, f.Supports(AssetKey{AssetType: "stock", Market: "HK"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "fund"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "stock", Market: "US"}), "sina 不支持 US")
}

func TestTencentFetcher_Supports(t *testing.T) {
	f := NewTencentFetcher(0)
	assert.True(t, f.Supports(AssetKey{AssetType: "stock", Market: "SH"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "fund"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "stock", Market: "US"}))
}

func TestEastmoneyFetcher_Source(t *testing.T) {
	assert.Equal(t, "api_eastmoney", NewEastmoneyFetcher(0).Source())
	assert.Equal(t, "api_sina", NewSinaFetcher(0).Source())
	assert.Equal(t, "api_tencent", NewTencentFetcher(0).Source())
}

// =====================================================================
// QuoteAggregator
// =====================================================================

// fakeFetcher 测试用 Fetcher：可控 Source、Supports、FetchOne 行为。
type fakeFetcher struct {
	source   string
	supports func(a AssetKey) bool
	fetch    func(ctx context.Context, a AssetKey) (*QuoteResult, error)
	calls    int32
}

func (f *fakeFetcher) Source() string                    { return f.source }
func (f *fakeFetcher) Supports(a AssetKey) bool          { return f.supports(a) }
func (f *fakeFetcher) FetchOne(ctx context.Context, a AssetKey) (*QuoteResult, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.fetch(ctx, a)
}

func TestNewAggregator_DefaultPoolSize(t *testing.T) {
	a, err := NewAggregator(nil, []string{"api_eastmoney"}, 0)
	require.NoError(t, err)
	assert.NotNil(t, a)
	a.Close()
}

func TestAggregator_FetchOne_ExplicitSource_Hit(t *testing.T) {
	em := &fakeFetcher{
		source:   "api_eastmoney",
		supports: func(a AssetKey) bool { return true },
		fetch: func(ctx context.Context, a AssetKey) (*QuoteResult, error) {
			return &QuoteResult{AssetID: a.AssetID, Price: decimal.RequireFromString("10")}, nil
		},
	}
	agg, _ := NewAggregator([]QuoteFetcher{em}, []string{"api_eastmoney"}, 4)
	defer agg.Close()

	r, err := agg.FetchOne(context.Background(), AssetKey{AssetID: 1, AssetType: "fund", AssetCode: "001"}, "api_eastmoney")
	require.NoError(t, err)
	assert.True(t, r.Price.Equal(decimal.RequireFromString("10")))
}

func TestAggregator_FetchOne_ExplicitSource_NotRegistered(t *testing.T) {
	agg, _ := NewAggregator(nil, nil, 4)
	defer agg.Close()

	_, err := agg.FetchOne(context.Background(), AssetKey{AssetType: "stock"}, "api_xxx")
	require.Error(t, err)
}

// 第一个源失败 → 自动降级到第二个（source="" 时按 priority 顺序）。
func TestAggregator_FetchOne_AutoSourceFallback(t *testing.T) {
	failOne := &fakeFetcher{
		source:   "api_eastmoney",
		supports: func(a AssetKey) bool { return true },
		fetch: func(ctx context.Context, a AssetKey) (*QuoteResult, error) {
			return nil, errors.New("eastmoney down")
		},
	}
	winSecond := &fakeFetcher{
		source:   "api_sina",
		supports: func(a AssetKey) bool { return true },
		fetch: func(ctx context.Context, a AssetKey) (*QuoteResult, error) {
			return &QuoteResult{AssetID: a.AssetID, Price: decimal.RequireFromString("99"), Source: "api_sina"}, nil
		},
	}
	agg, _ := NewAggregator([]QuoteFetcher{failOne, winSecond}, []string{"api_eastmoney", "api_sina"}, 4)
	defer agg.Close()

	r, err := agg.FetchOne(context.Background(), AssetKey{AssetID: 1, AssetType: "stock", Market: "SH"}, "")
	require.NoError(t, err)
	assert.True(t, r.Price.Equal(decimal.RequireFromString("99")))
	assert.Equal(t, int32(1), atomic.LoadInt32(&failOne.calls))
	assert.Equal(t, int32(1), atomic.LoadInt32(&winSecond.calls))
}

// 都不支持时 → 返回 ErrUnsupportedAsset。
func TestAggregator_FetchOne_AutoSource_AllUnsupported(t *testing.T) {
	notSupport := &fakeFetcher{
		source:   "api_eastmoney",
		supports: func(a AssetKey) bool { return false },
		fetch: func(ctx context.Context, a AssetKey) (*QuoteResult, error) {
			return nil, errors.New("unreachable")
		},
	}
	agg, _ := NewAggregator([]QuoteFetcher{notSupport}, []string{"api_eastmoney"}, 4)
	defer agg.Close()

	_, err := agg.FetchOne(context.Background(), AssetKey{AssetType: "stock"}, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnsupportedAsset))
}

// FetchBatch：并发处理多条，每条对应位置正确，单条失败不影响其他。
func TestAggregator_FetchBatch_PartialFailure(t *testing.T) {
	em := &fakeFetcher{
		source:   "api_eastmoney",
		supports: func(a AssetKey) bool { return true },
		fetch: func(ctx context.Context, a AssetKey) (*QuoteResult, error) {
			if a.AssetID == 99 {
				return nil, errors.New("simulated 404")
			}
			return &QuoteResult{AssetID: a.AssetID, Price: decimal.NewFromInt(int64(a.AssetID)), Source: "api_eastmoney"}, nil
		},
	}
	agg, _ := NewAggregator([]QuoteFetcher{em}, []string{"api_eastmoney"}, 4)
	defer agg.Close()

	keys := []AssetKey{
		{AssetID: 1, AssetType: "fund", AssetCode: "001"},
		{AssetID: 99, AssetType: "fund", AssetCode: "002"},
		{AssetID: 3, AssetType: "fund", AssetCode: "003"},
	}
	results := agg.FetchBatch(context.Background(), keys, "api_eastmoney")
	require.Len(t, results, 3)

	assert.Equal(t, uint(1), results[0].AssetID)
	require.NoError(t, results[0].Err)
	assert.True(t, results[0].Price.Equal(decimal.NewFromInt(1)))

	assert.Equal(t, uint(99), results[1].AssetID)
	require.Error(t, results[1].Err)

	assert.Equal(t, uint(3), results[2].AssetID)
	require.NoError(t, results[2].Err)
}

func TestAggregator_SourcePriority_ReturnsCopy(t *testing.T) {
	prio := []string{"a", "b"}
	agg, _ := NewAggregator(nil, prio, 4)
	defer agg.Close()

	got := agg.SourcePriority()
	got[0] = "modified"
	// 原 prio 不应被修改
	assert.Equal(t, "a", agg.SourcePriority()[0])
}

// mapEastmoneyMarket：覆盖各市场分支。
func TestMapEastmoneyMarket(t *testing.T) {
	cases := map[string]string{
		"SH": "1",
		"sh": "1",
		"SZ": "0",
		"BJ": "0",
		"HK": "116",
		"US": "105",
		"":   "",
		"XX": "",
	}
	for in, want := range cases {
		assert.Equalf(t, want, mapEastmoneyMarket(in), "market=%q", in)
	}
}

// 非阻塞 timeout 防呆：FetchOne 的 ctx 可被取消。
func TestAggregator_FetchOne_RespectsCtx(t *testing.T) {
	ff := &fakeFetcher{
		source:   "api_eastmoney",
		supports: func(a AssetKey) bool { return true },
		fetch: func(ctx context.Context, a AssetKey) (*QuoteResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return &QuoteResult{AssetID: a.AssetID}, nil
			}
		},
	}
	agg, _ := NewAggregator([]QuoteFetcher{ff}, []string{"api_eastmoney"}, 4)
	defer agg.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := agg.FetchOne(ctx, AssetKey{AssetID: 1, AssetType: "fund"}, "api_eastmoney")
	require.Error(t, err)
}
