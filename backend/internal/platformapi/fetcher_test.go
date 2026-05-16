package platformapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// EastmoneyFetcher.fetchFund —— jsonp 解析
// =====================================================================

func TestEastmoneyFetcher_FetchFund_HappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/js/110022.js", r.URL.Path)
		fmt.Fprint(w, `jsonpgz({"fundcode":"110022","name":"易方达消费行业","jzrq":"2026-05-15","dwjz":"2.6512","gsz":"2.6800","gszzl":"-0.32","gztime":"2026-05-15 15:00"});`)
	}))
	defer ts.Close()

	f := NewEastmoneyFetcher(2*time.Second, WithFundBaseURL(ts.URL))
	res, err := f.FetchOne(context.Background(), AssetKey{
		AssetID: 1, AssetType: "fund", AssetCode: "110022",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.True(t, res.Price.Equal(decimal.RequireFromString("2.68")), "price gsz preferred, got %s", res.Price)
	assert.True(t, res.ChangePct.Equal(decimal.RequireFromString("-0.32")))
	assert.Equal(t, "api_eastmoney", res.Source)
	// gztime 解析（按本地时区）
	assert.Equal(t, 2026, res.QuoteTime.Year())
	assert.Equal(t, 15, res.QuoteTime.Hour())
}

func TestEastmoneyFetcher_FetchFund_OnlyDwjz_FallbacksToDwjz(t *testing.T) {
	// 没有 gsz 字段，应降级用 dwjz
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `jsonpgz({"fundcode":"000001","name":"测试","dwjz":"1.2345"});`)
	}))
	defer ts.Close()

	f := NewEastmoneyFetcher(2*time.Second, WithFundBaseURL(ts.URL))
	res, err := f.FetchOne(context.Background(), AssetKey{AssetType: "fund", AssetCode: "000001"})
	require.NoError(t, err)
	assert.True(t, res.Price.Equal(decimal.RequireFromString("1.2345")))
}

func TestEastmoneyFetcher_FetchFund_EmptyBody_ReturnsErrNoData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 空响应
	}))
	defer ts.Close()

	f := NewEastmoneyFetcher(time.Second, WithFundBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "fund", AssetCode: "001"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoData)
}

func TestEastmoneyFetcher_FetchFund_MalformedBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not jsonp at all`)
	}))
	defer ts.Close()

	f := NewEastmoneyFetcher(time.Second, WithFundBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "fund", AssetCode: "001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected body")
}

func TestEastmoneyFetcher_FetchFund_EmptyAssetCode_Errors(t *testing.T) {
	f := NewEastmoneyFetcher(0)
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "fund", AssetCode: ""})
	require.Error(t, err)
}

// =====================================================================
// EastmoneyFetcher.fetchStock —— f43 单位换算（分→元）
// =====================================================================

func TestEastmoneyFetcher_FetchStock_PriceCentsToYuan(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "secid=1.600519")
		// f43=172800（分）= 1728.00 元；f170=-150（百分比 ×100）→ -1.50%
		fmt.Fprint(w, `{"data":{"f43":172800,"f44":173000,"f45":172000,"f57":"600519","f58":"贵州茅台","f86":1234567,"f168":0,"f169":0,"f170":-150}}`)
	}))
	defer ts.Close()

	f := NewEastmoneyFetcher(2*time.Second, WithStockBaseURL(ts.URL))
	res, err := f.FetchOne(context.Background(), AssetKey{
		AssetID: 100, AssetType: "stock", AssetCode: "600519", Market: "SH",
	})
	require.NoError(t, err)
	assert.True(t, res.Price.Equal(decimal.RequireFromString("1728")), "price=%s, want 1728 (172800/100)", res.Price)
	assert.True(t, res.ChangePct.Equal(decimal.RequireFromString("-1.5")), "changePct=%s, want -1.5 (-150/100)", res.ChangePct)
	assert.True(t, res.Volume.Equal(decimal.RequireFromString("1234567")))
	assert.Equal(t, "api_eastmoney", res.Source)
}

func TestEastmoneyFetcher_FetchStock_UnsupportedMarket_Errors(t *testing.T) {
	f := NewEastmoneyFetcher(0)
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", AssetCode: "600519", Market: "XX"})
	require.Error(t, err)
}

func TestEastmoneyFetcher_FetchStock_NoData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"rc":0}`)
	}))
	defer ts.Close()

	f := NewEastmoneyFetcher(time.Second, WithStockBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", AssetCode: "600519", Market: "SH"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoData)
}

// =====================================================================
// SinaFetcher —— CSV 解析
// =====================================================================

func TestSinaFetcher_FetchStock_AShare(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sina 的 URL 形如 /list=sh600519 —— "list=..." 在 path 中
		assert.Contains(t, r.URL.RequestURI(), "list=sh600519")
		// fields[0]=名称 fields[1]=今开 fields[2]=昨收 fields[3]=当前价 ... fields[8]=成交量
		fmt.Fprint(w, `var hq_str_sh600519="贵州茅台,1700.00,1690.00,1728.00,1720.00,1680.00,1727.99,1728.01,1234567,2100000000.00,extra,extra,extra";`)
	}))
	defer ts.Close()

	f := NewSinaFetcher(2*time.Second, WithSinaBaseURL(ts.URL))
	res, err := f.FetchOne(context.Background(), AssetKey{
		AssetID: 1, AssetType: "stock", AssetCode: "600519", Market: "SH",
	})
	require.NoError(t, err)
	assert.True(t, res.Price.Equal(decimal.RequireFromString("1728.00")))
	// changePct = (1728 - 1690) / 1690 * 100 = 2.2485...
	expected := decimal.RequireFromString("1728").Sub(decimal.RequireFromString("1690")).
		Div(decimal.RequireFromString("1690")).
		Mul(decimal.NewFromInt(100)).Round(4)
	assert.True(t, res.ChangePct.Equal(expected), "changePct=%s, want %s", res.ChangePct, expected)
	assert.True(t, res.Volume.Equal(decimal.RequireFromString("1234567")))
	assert.Equal(t, "api_sina", res.Source)
}

func TestSinaFetcher_FetchStock_HKMarket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RequestURI(), "list=hk00700")
		// 港股：fields[6] 是当前价
		fmt.Fprint(w, `var hq_str_hk00700="腾讯控股,300.00,310.00,295.00,305.00,290.00,302.50,extra,extra";`)
	}))
	defer ts.Close()

	f := NewSinaFetcher(2*time.Second, WithSinaBaseURL(ts.URL))
	res, err := f.FetchOne(context.Background(), AssetKey{
		AssetID: 1, AssetType: "stock", AssetCode: "00700", Market: "HK",
	})
	require.NoError(t, err)
	assert.True(t, res.Price.Equal(decimal.RequireFromString("302.50")), "got %s", res.Price)
	// HK 不读 volume
	assert.True(t, res.Volume.IsZero())
}

func TestSinaFetcher_FetchStock_EmptyBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	f := NewSinaFetcher(time.Second, WithSinaBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", AssetCode: "600519", Market: "SH"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoData)
}

func TestSinaFetcher_FetchStock_MalformedBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `garbage no equals quotes`)
	}))
	defer ts.Close()

	f := NewSinaFetcher(time.Second, WithSinaBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", AssetCode: "600519", Market: "SH"})
	require.Error(t, err)
}

func TestSinaFetcher_FetchStock_BadPrice(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `var hq_str_sh600519="贵州茅台,1700.00,1690.00,not-a-number,1720.00,1680.00,1727.99,1728.01,1234567";`)
	}))
	defer ts.Close()

	f := NewSinaFetcher(time.Second, WithSinaBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", AssetCode: "600519", Market: "SH"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad price")
}

// =====================================================================
// TencentFetcher —— ~ 分隔解析
// =====================================================================

func TestTencentFetcher_FetchStock_AShare(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RequestURI(), "q=sh600519")
		// parts[3]=当前价 parts[4]=昨收 parts[5]=今开 parts[6]=成交量(手)
		fmt.Fprint(w, `v_sh600519="1~贵州茅台~600519~1728.00~1690.00~1700.00~12345~more~fields~here";`)
	}))
	defer ts.Close()

	f := NewTencentFetcher(2*time.Second, WithTencentBaseURL(ts.URL))
	res, err := f.FetchOne(context.Background(), AssetKey{
		AssetID: 1, AssetType: "stock", AssetCode: "600519", Market: "SH",
	})
	require.NoError(t, err)
	assert.True(t, res.Price.Equal(decimal.RequireFromString("1728.00")))
	// changePct = (1728 - 1690) / 1690 * 100
	expected := decimal.RequireFromString("1728").Sub(decimal.RequireFromString("1690")).
		Div(decimal.RequireFromString("1690")).
		Mul(decimal.NewFromInt(100)).Round(4)
	assert.True(t, res.ChangePct.Equal(expected))
	assert.True(t, res.Volume.Equal(decimal.RequireFromString("12345")))
	assert.Equal(t, "api_tencent", res.Source)
}

func TestTencentFetcher_FetchStock_BadPrice(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `v_sh600519="1~贵州茅台~600519~not_number~1690.00~1700.00~12345~";`)
	}))
	defer ts.Close()

	f := NewTencentFetcher(time.Second, WithTencentBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", AssetCode: "600519", Market: "SH"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad price")
}

func TestTencentFetcher_FetchStock_TooFewFields(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `v_sh600519="1~少~字段";`)
	}))
	defer ts.Close()

	f := NewTencentFetcher(time.Second, WithTencentBaseURL(ts.URL))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", AssetCode: "600519", Market: "SH"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoData)
}

func TestTencentFetcher_FetchStock_EmptyAssetCode(t *testing.T) {
	f := NewTencentFetcher(0)
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "stock", Market: "SH", AssetCode: ""})
	require.Error(t, err)
}

// =====================================================================
// 选项处理：trimRightSlash / 不传选项保持默认（行为不变）
// =====================================================================

func TestWithBaseURL_TrimsTrailingSlashes(t *testing.T) {
	// 通过实际 fetcher 行为验证：传带尾斜杠的 URL 仍能正常拼出 /js/{code}.js
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 路径里只有一个 /js/，不应有 //js/
		assert.False(t, strings.HasPrefix(r.URL.Path, "//"), "path should not have double slash, got %s", r.URL.Path)
		fmt.Fprint(w, `jsonpgz({"fundcode":"001","gsz":"1.0"});`)
	}))
	defer ts.Close()

	f := NewEastmoneyFetcher(time.Second, WithFundBaseURL(ts.URL+"///"))
	_, err := f.FetchOne(context.Background(), AssetKey{AssetType: "fund", AssetCode: "001"})
	require.NoError(t, err)
}

func TestNewFetcher_NoOptions_KeepsDefaultBaseURL(t *testing.T) {
	// 不传 opts 时不应 panic（不验证联网，仅构造）
	assert.NotNil(t, NewEastmoneyFetcher(time.Second))
	assert.NotNil(t, NewSinaFetcher(time.Second))
	assert.NotNil(t, NewTencentFetcher(time.Second))
}
