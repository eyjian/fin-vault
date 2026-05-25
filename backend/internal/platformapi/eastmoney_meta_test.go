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

// =====================================================================
// EastmoneyMetaFetcher.fetchFundMeta —— pingzhongdata 解析
// =====================================================================

func TestEastmoneyMetaFetcher_FetchFund_HappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pingzhongdata/110022.js", r.URL.Path)
		fmt.Fprint(w, `
var fS_name = "易方达消费行业";
var fS_code = "110022";
var jjglr   = "易方达基金";
var Data_currentFundManager = [{"id":"30198751","name":"萧楠","star":4,"workTime":"3650","power":{"avr":"7.18%"}}];
var fS_jjlx = "股票型";
`)
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(2*time.Second, WithFundDetailBaseURL(ts.URL))
	meta, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "110022",
	})
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "易方达消费行业", meta.Name)
	assert.Equal(t, "易方达基金", meta.Company)
	assert.Equal(t, "萧楠", meta.Manager)
	assert.Equal(t, "equity", meta.FundType)
	assert.Equal(t, "api_eastmoney", meta.Source)
}

func TestEastmoneyMetaFetcher_FetchFund_PartialFields_GracefulDegrade(t *testing.T) {
	// 只有 fS_name，其他字段全部缺失：应当返回成功，仅含 Name + Source
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `var fS_name = "测试基金";`)
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(time.Second, WithFundDetailBaseURL(ts.URL))
	meta, err := f.FetchMeta(context.Background(), AssetKey{AssetType: "fund", AssetCode: "001"})
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "测试基金", meta.Name)
	assert.Equal(t, "", meta.Company)
	assert.Equal(t, "", meta.Manager)
	assert.Equal(t, "", meta.FundType)
}

func TestEastmoneyMetaFetcher_FetchFund_NoData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 空响应
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(time.Second, WithFundDetailBaseURL(ts.URL))
	_, err := f.FetchMeta(context.Background(), AssetKey{AssetType: "fund", AssetCode: "001"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoData)
}

func TestEastmoneyMetaFetcher_FetchFund_OnlyNameMissing_ReturnsErrNoData(t *testing.T) {
	// 有 jjglr 但没有 fS_name，按设计也算无数据
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `var jjglr="易方达基金";`)
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(time.Second, WithFundDetailBaseURL(ts.URL))
	_, err := f.FetchMeta(context.Background(), AssetKey{AssetType: "fund", AssetCode: "001"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoData)
}

// =====================================================================
// EastmoneyMetaFetcher.fetchStockMeta —— stock/get 字段解析
// =====================================================================

func TestEastmoneyMetaFetcher_FetchStock_HappyPath_AShare(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/qt/stock/get", r.URL.Path)
		assert.Contains(t, r.URL.RawQuery, "secid=1.600519")
		assert.Contains(t, r.URL.RawQuery, "fields=f43,f57,f58,f127,f128,f189")
		fmt.Fprint(w, `{"data":{"f43":172800,"f57":"600519","f58":"贵州茅台","f127":"白酒","f128":"消费","f189":20010827}}`)
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(2*time.Second, WithStockBaseURL(ts.URL))
	meta, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "600519", Market: "SH",
	})
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "贵州茅台", meta.Name)
	assert.Equal(t, "SH", meta.Market)
	assert.Equal(t, "白酒", meta.Industry)
	assert.Equal(t, "消费", meta.Sector)
	assert.True(t, meta.LatestPrice.Equal(decimal.RequireFromString("1728.00")), "got %s", meta.LatestPrice)
	assert.Equal(t, 2001, meta.ListingDate.Year())
	assert.Equal(t, time.Month(8), meta.ListingDate.Month())
	assert.Equal(t, 27, meta.ListingDate.Day())
	assert.Equal(t, "api_eastmoney", meta.Source)
}

func TestEastmoneyMetaFetcher_FetchStock_HKMarket_NoIndustrySector(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "secid=116.00700")
		fmt.Fprint(w, `{"data":{"f43":30000,"f57":"00700","f58":"腾讯控股","f127":"互联网","f128":"科技"}}`)
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(time.Second, WithStockBaseURL(ts.URL))
	meta, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "00700", Market: "HK",
	})
	require.NoError(t, err)
	require.NotNil(t, meta)

	// HK 市场不返回 industry / sector（即使远端返回也忽略）
	assert.Equal(t, "腾讯控股", meta.Name)
	assert.Equal(t, "HK", meta.Market)
	assert.Equal(t, "", meta.Industry)
	assert.Equal(t, "", meta.Sector)
}

func TestEastmoneyMetaFetcher_FetchStock_InferMarketByCodePrefix(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 0 开头 → SZ → secid prefix=0
		assert.Contains(t, r.URL.RawQuery, "secid=0.000001")
		fmt.Fprint(w, `{"data":{"f43":1200,"f57":"000001","f58":"平安银行","f127":"银行","f128":"金融"}}`)
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(time.Second, WithStockBaseURL(ts.URL))
	// 不传 market，由 inferStockMarket 推断
	meta, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "000001",
	})
	require.NoError(t, err)
	assert.Equal(t, "SZ", meta.Market)
	assert.Equal(t, "平安银行", meta.Name)
	assert.Equal(t, "银行", meta.Industry)
}

func TestEastmoneyMetaFetcher_FetchStock_NoData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`) // 没有 "data" 字段
	}))
	defer ts.Close()

	f := NewEastmoneyMetaFetcher(time.Second, WithStockBaseURL(ts.URL))
	_, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "600519", Market: "SH",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoData)
}

func TestEastmoneyMetaFetcher_UnsupportedAssetType(t *testing.T) {
	f := NewEastmoneyMetaFetcher(time.Second)
	_, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "wealth", AssetCode: "LC202604001",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedAsset)

	_, err = f.FetchMeta(context.Background(), AssetKey{
		AssetType: "cash", AssetCode: "CASH-tb-CNY",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedAsset)
}

func TestEastmoneyMetaFetcher_EmptyAssetCode(t *testing.T) {
	f := NewEastmoneyMetaFetcher(time.Second)
	_, err := f.FetchMeta(context.Background(), AssetKey{AssetType: "fund", AssetCode: ""})
	require.Error(t, err)
}

func TestEastmoneyMetaFetcher_Source(t *testing.T) {
	f := NewEastmoneyMetaFetcher(0)
	assert.Equal(t, "api_eastmoney", f.Source())
	assert.True(t, f.Supports(AssetKey{AssetType: "fund"}))
	assert.True(t, f.Supports(AssetKey{AssetType: "stock"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "wealth"}))
	assert.False(t, f.Supports(AssetKey{AssetType: "cash"}))
}

func TestInferStockMarket(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{"600519", "SH"},
		{"688981", "SH"},
		{"000001", "SZ"},
		{"300750", "SZ"},
		{"830799", "BJ"},
		{"430047", "BJ"},
		// 港股：5 位纯数字（不符合 A 股 6 位 / 北交所规则）→ HK
		{"00700", "HK"},
		{"09988", "HK"},
		{"01810", "HK"},
		// 港美股代码不应交给本函数推断，调用方需先判断 market 是否已选择；
		// 这里仅记录"0 开头一律 SZ"的语义，其它字母开头返回空。
		{"AAPL", ""},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, inferStockMarket(c.code), "code=%s", c.code)
	}
}

func TestMapFundTypeLabel(t *testing.T) {
	cases := map[string]string{
		"股票型":     "equity",
		"债券型":     "bond",
		"混合型":     "hybrid",
		"货币型":     "money",
		"指数型":     "index",
		"QDII":    "qdii",
		"qdii基金":  "qdii",
		"unknown": "unknown",
	}
	for in, want := range cases {
		assert.Equal(t, want, mapFundTypeLabel(in), "input=%s", in)
	}
}
