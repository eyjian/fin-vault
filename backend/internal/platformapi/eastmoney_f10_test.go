package platformapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 真实从东方财富 datacenter F10 获取到的 002190 返回（用于回归测试）
const realF10Body = `{"version":"400d866b869f5bba334e016ffdb4dde0","result":{"pages":1,"data":[{"SECUCODE":"002190.SZ","SECURITY_NAME_ABBR":"成飞集成","INDUSTRYCSRC1":"制造业-汽车制造业","EM2016":"电气设备-其他电气设备-其他电气设备","LISTING_DATE":"2007-12-03 00:00:00"}],"count":1},"success":true,"message":"ok","code":0}`

// TestEastmoneyF10Enricher_RealBody 用真实 datacenter 返回内容验证解析正确性。
func TestEastmoneyF10Enricher_RealBody(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/securities/api/data/v1/get", r.URL.Path)
		assert.Contains(t, r.URL.RawQuery, "002190.SZ")
		assert.Contains(t, r.URL.RawQuery, "RPT_F10_BASIC_ORGINFO")
		fmt.Fprint(w, realF10Body)
	}))
	defer ts.Close()

	enricher := NewEastmoneyF10Enricher(2*time.Second, WithStockF10BaseURL(ts.URL))
	meta := &AssetMeta{
		Name:        "成飞集成",
		Source:      "api_sina",
		Market:      "SZ",
		LatestPrice: decimal.RequireFromString("29.41"),
		// industry / sector / listing_date 都为空，模拟主源（包括降级到的新浪）只返回名称+价格
	}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "002190", Market: "SZ",
	}, meta)
	require.NoError(t, err)

	assert.Equal(t, "制造业-汽车制造业", meta.Industry)
	assert.Equal(t, "电气设备-其他电气设备-其他电气设备", meta.Sector)
	assert.Equal(t, 2007, meta.ListingDate.Year())
	assert.Equal(t, time.Month(12), meta.ListingDate.Month())
	assert.Equal(t, 3, meta.ListingDate.Day())
	assert.Equal(t, 1, calls)

	// 主源已有的字段不被覆盖
	assert.Equal(t, "成飞集成", meta.Name)
	assert.Equal(t, "api_sina", meta.Source)
	assert.True(t, meta.LatestPrice.Equal(decimal.RequireFromString("29.41")))
}

// TestEastmoneyF10Enricher_OnlyFillEmpty 验证 enricher 只填空、不覆盖。
func TestEastmoneyF10Enricher_OnlyFillEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, realF10Body)
	}))
	defer ts.Close()

	enricher := NewEastmoneyF10Enricher(time.Second, WithStockF10BaseURL(ts.URL))
	existingDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)
	meta := &AssetMeta{
		Industry:    "已有行业",
		Sector:      "已有板块",
		ListingDate: existingDate,
	}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "002190", Market: "SZ",
	}, meta)
	require.NoError(t, err)

	assert.Equal(t, "已有行业", meta.Industry)
	assert.Equal(t, "已有板块", meta.Sector)
	assert.Equal(t, 2020, meta.ListingDate.Year())
}

// TestEastmoneyF10Enricher_SkipNonAShare 验证非 A 股直接跳过，不发请求。
func TestEastmoneyF10Enricher_SkipNonAShare(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, realF10Body)
	}))
	defer ts.Close()

	enricher := NewEastmoneyF10Enricher(time.Second, WithStockF10BaseURL(ts.URL))

	// 港股
	hkMeta := &AssetMeta{Name: "腾讯控股", Market: "HK"}
	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "00700", Market: "HK",
	}, hkMeta)
	require.NoError(t, err)
	assert.Equal(t, "", hkMeta.Industry)

	// 美股
	usMeta := &AssetMeta{Name: "Apple", Market: "US"}
	err = enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "AAPL", Market: "US",
	}, usMeta)
	require.NoError(t, err)
	assert.Equal(t, "", usMeta.Industry)

	// 基金
	fundMeta := &AssetMeta{Name: "易方达消费"}
	err = enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "110022",
	}, fundMeta)
	require.NoError(t, err)
	assert.Equal(t, "", fundMeta.Industry)

	assert.Equal(t, 0, calls, "非 A 股不应发请求")
}

// TestEastmoneyF10Enricher_SkipWhenAllFilled 验证 meta 已经满字段时不发请求。
func TestEastmoneyF10Enricher_SkipWhenAllFilled(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, realF10Body)
	}))
	defer ts.Close()

	enricher := NewEastmoneyF10Enricher(time.Second, WithStockF10BaseURL(ts.URL))
	meta := &AssetMeta{
		Industry:    "汽车制造",
		Sector:      "汽车零部件",
		ListingDate: time.Date(2009, 8, 18, 0, 0, 0, 0, time.Local),
	}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "002190", Market: "SZ",
	}, meta)
	require.NoError(t, err)
	assert.Equal(t, 0, calls, "已满字段时不应发请求")
}

// TestEastmoneyF10Enricher_NetworkErrorDoesNotPropagate 验证网络/HTTP 错误不返回 error。
func TestEastmoneyF10Enricher_NetworkErrorDoesNotPropagate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	enricher := NewEastmoneyF10Enricher(time.Second, WithStockF10BaseURL(ts.URL))
	meta := &AssetMeta{Name: "成飞集成", Market: "SZ"}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "002190", Market: "SZ",
	}, meta)
	require.NoError(t, err, "F10 失败不应返回 error，仅 graceful degrade")
	assert.Equal(t, "", meta.Industry)
	assert.Equal(t, "", meta.Sector)
	assert.True(t, meta.ListingDate.IsZero())
}

// TestEastmoneyF10Enricher_NilMetaIsSafe 验证 nil meta 不 panic。
func TestEastmoneyF10Enricher_NilMetaIsSafe(t *testing.T) {
	enricher := NewEastmoneyF10Enricher(time.Second)
	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "002190", Market: "SZ",
	}, nil)
	require.NoError(t, err)
}
