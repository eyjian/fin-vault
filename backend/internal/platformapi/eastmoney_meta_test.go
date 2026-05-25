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

// TestEastmoneyMetaFetcher_FetchStock_F10Enrich 验证当 push2 缺 f127/f128/f189 时，
// 自动调用 datacenter F10 接口补全行业 / 板块 / 上市日。
//
// 这是真实场景：东方财富 push2 是行情快照接口，对绝大多数 A 股的 f127/f128/f189 返回为空，
// 必须借助 F10 基本资料接口才能拿到完整的基本面信息。
func TestEastmoneyMetaFetcher_FetchStock_F10Enrich(t *testing.T) {
	// push2 mock：故意只返回名称和价格，f127/f128/f189 全部缺失（模拟真实 002190 场景）
	pushTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/qt/stock/get", r.URL.Path)
		assert.Contains(t, r.URL.RawQuery, "secid=0.002190")
		fmt.Fprint(w, `{"data":{"f43":2941,"f57":"002190","f58":"成飞集成"}}`)
	}))
	defer pushTS.Close()

	// F10 mock：返回完整基本资料（行业 / 板块 / 上市日）
	f10Calls := 0
	f10TS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f10Calls++
		assert.Equal(t, "/securities/api/data/v1/get", r.URL.Path)
		// SECUCODE 编码后形如 %22002190.SZ%22，filter 参数包含这串
		assert.Contains(t, r.URL.RawQuery, "002190.SZ")
		assert.Contains(t, r.URL.RawQuery, "RPT_F10_BASIC_ORGINFO")
		fmt.Fprint(w, `{"result":{"data":[{"SECUCODE":"002190.SZ","SECURITY_CODE":"002190","SECURITY_NAME_ABBR":"成飞集成","INDUSTRYCSRC1":"航空装备","EM2016":"国防军工","LISTING_DATE":"2009-08-18 00:00:00"}]}}`)
	}))
	defer f10TS.Close()

	f := NewEastmoneyMetaFetcher(2*time.Second,
		WithStockBaseURL(pushTS.URL),
		WithStockF10BaseURL(f10TS.URL),
	)
	meta, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "002190", Market: "SZ",
	})
	require.NoError(t, err)
	require.NotNil(t, meta)

	// 名称和价格来自 push2
	assert.Equal(t, "成飞集成", meta.Name)
	assert.True(t, meta.LatestPrice.Equal(decimal.RequireFromString("29.41")), "got %s", meta.LatestPrice)

	// 行业 / 板块 / 上市日通过 F10 补全
	assert.Equal(t, "航空装备", meta.Industry)
	assert.Equal(t, "国防军工", meta.Sector)
	assert.Equal(t, 2009, meta.ListingDate.Year())
	assert.Equal(t, time.Month(8), meta.ListingDate.Month())
	assert.Equal(t, 18, meta.ListingDate.Day())

	assert.Equal(t, 1, f10Calls, "F10 应该被调用了一次")
}

// TestEastmoneyMetaFetcher_FetchStock_F10NotCalledWhenPush2Complete 验证当 push2
// 已返回完整 f127/f128/f189 时，**不会**调用 F10 接口（性能优化，避免无谓的额外 HTTP 请求）。
func TestEastmoneyMetaFetcher_FetchStock_F10NotCalledWhenPush2Complete(t *testing.T) {
	pushTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"f43":172800,"f57":"600519","f58":"贵州茅台","f127":"白酒","f128":"消费","f189":20010827}}`)
	}))
	defer pushTS.Close()

	f10Calls := 0
	f10TS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f10Calls++
		fmt.Fprint(w, `{"result":{"data":[]}}`)
	}))
	defer f10TS.Close()

	f := NewEastmoneyMetaFetcher(2*time.Second,
		WithStockBaseURL(pushTS.URL),
		WithStockF10BaseURL(f10TS.URL),
	)
	meta, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "600519", Market: "SH",
	})
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "白酒", meta.Industry)
	assert.Equal(t, "消费", meta.Sector)
	assert.Equal(t, 0, f10Calls, "push2 已满字段时不应调用 F10")
}

// TestEastmoneyMetaFetcher_FetchStock_F10FailDoesNotBreakMain 验证 F10 接口失败
// 不会影响主路径，meta 仍正常返回（部分字段为空），实现 graceful degrade。
func TestEastmoneyMetaFetcher_FetchStock_F10FailDoesNotBreakMain(t *testing.T) {
	pushTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"f43":2941,"f57":"002190","f58":"成飞集成"}}`)
	}))
	defer pushTS.Close()

	// F10 故意返回 500
	f10TS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer f10TS.Close()

	f := NewEastmoneyMetaFetcher(2*time.Second,
		WithStockBaseURL(pushTS.URL),
		WithStockF10BaseURL(f10TS.URL),
	)
	meta, err := f.FetchMeta(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "002190", Market: "SZ",
	})
	require.NoError(t, err, "F10 失败不应中断主路径")
	require.NotNil(t, meta)

	// 主字段（名称、价格）仍能拿到
	assert.Equal(t, "成飞集成", meta.Name)
	assert.True(t, meta.LatestPrice.Equal(decimal.RequireFromString("29.41")))

	// F10 补全字段为空（graceful degrade）
	assert.Equal(t, "", meta.Industry)
	assert.Equal(t, "", meta.Sector)
	assert.True(t, meta.ListingDate.IsZero())
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
