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

// fakeJbgkHTML 模拟 fundf10.eastmoney.com/jbgk_011022.html 真实页面结构。
//
// 字段对照：
//   - 基金简称  → meta.Name
//   - 基金类型  → meta.FundType ("混合型-偏股" → "hybrid")
//   - 基金管理人 → meta.Company
//   - 基金经理人 → meta.Manager
//   - 业绩比较基准 → meta.Benchmark
const fakeJbgkHTML = `<!DOCTYPE html><html><head><title>011022</title></head><body>
<table class="info w790">
<tr><th>基金全称</th><td colspan="3">汇添富互联网核心资产六个月持有期混合型证券投资基金</td></tr>
<tr><th>基金简称</th><td colspan="3">汇添富互联网核心资产六个月持有混合C</td></tr>
<tr><th>基金代码</th><td>011022（前端）</td><th>基金类型</th><td>混合型-偏股</td></tr>
<tr><th>发行日期</th><td>2021年01月20日</td><th>成立日期/规模</th><td>2021年01月25日 / 65.737亿份</td></tr>
<tr><th>基金管理人</th><td><a href="#">汇添富基金</a></td><th>基金托管人</th><td>中国银行</td></tr>
<tr><th>基金经理人</th><td><a href="#">沈若雨</a></td><th>成立来分红</th><td>每份累计0.00元（0次）</td></tr>
<tr><th>业绩比较基准</th><td colspan="3">中证互联网指数收益率*50%+恒生科技指数收益率*30%+中债综合指数收益率*20%</td></tr>
</table></body></html>`

// fakeFundgzJS 模拟 fundgz.1234567.com.cn/js/011022.js 真实返回。
const fakeFundgzJS = `jsonpgz({"fundcode":"011022","name":"汇添富互联网核心资产六个月持有混合C","jzrq":"2026-05-22","dwjz":"1.3742","gsz":"1.4144","gszzl":"2.92","gztime":"2026-05-25 15:00"});`

// newFundDetailTestServer 启动一个 mock httptest server，
// 同时响应 jbgk HTML 和 fundgz JS 两类路径。返回 server 与请求计数器。
func newFundDetailTestServer(t *testing.T, jbgkBody, gzBody string) (*httptest.Server, *int, *int) {
	t.Helper()
	jbgkCalls, gzCalls := 0, 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/jbgk_"):
			jbgkCalls++
			fmt.Fprint(w, jbgkBody)
		case strings.HasPrefix(r.URL.Path, "/js/"):
			gzCalls++
			fmt.Fprint(w, gzBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)
	return ts, &jbgkCalls, &gzCalls
}

// TestEastmoneyFundDetailEnricher_AllFieldsFilled 验证主源拿到名称后，
// 由 enricher 把公司 / 经理 / 类型 / 业绩基准 / 净值 / 净值日期都补齐。
func TestEastmoneyFundDetailEnricher_AllFieldsFilled(t *testing.T) {
	ts, jbgkCalls, gzCalls := newFundDetailTestServer(t, fakeJbgkHTML, fakeFundgzJS)

	enricher := NewEastmoneyFundDetailEnricher(2*time.Second,
		WithFundJbgkBaseURL(ts.URL),
		WithFundBaseURL(ts.URL))
	meta := &AssetMeta{
		Name:   "汇添富互联网核心资产六个月持有混合C",
		Source: "api_eastmoney",
	}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "011022",
	}, meta)
	require.NoError(t, err)

	assert.Equal(t, "汇添富基金", meta.Company)
	assert.Equal(t, "沈若雨", meta.Manager)
	assert.Equal(t, "hybrid", meta.FundType)
	assert.Contains(t, meta.Benchmark, "中证互联网")
	assert.True(t, meta.LatestNAV.Equal(decimal.RequireFromString("1.3742")), "got %s", meta.LatestNAV)
	assert.Equal(t, 2026, meta.NAVDate.Year())
	assert.Equal(t, time.Month(5), meta.NAVDate.Month())
	assert.Equal(t, 22, meta.NAVDate.Day())
	assert.Equal(t, 1, *jbgkCalls)
	assert.Equal(t, 1, *gzCalls)
}

// TestEastmoneyFundDetailEnricher_OnlyFillEmpty 验证不会覆盖主源已有的值。
func TestEastmoneyFundDetailEnricher_OnlyFillEmpty(t *testing.T) {
	ts, _, _ := newFundDetailTestServer(t, fakeJbgkHTML, fakeFundgzJS)

	enricher := NewEastmoneyFundDetailEnricher(time.Second,
		WithFundJbgkBaseURL(ts.URL),
		WithFundBaseURL(ts.URL))
	existingNAV := decimal.RequireFromString("1.234")
	existingDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)
	meta := &AssetMeta{
		Name:      "用户原值",
		Company:   "已有公司",
		Manager:   "已有经理",
		FundType:  "equity", // 主源已确定股票型，enricher 不应覆盖为 hybrid
		Benchmark: "已有基准",
		RiskLevel: "中高",
		LatestNAV: existingNAV,
		NAVDate:   existingDate,
	}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "011022",
	}, meta)
	require.NoError(t, err)

	assert.Equal(t, "用户原值", meta.Name)
	assert.Equal(t, "已有公司", meta.Company)
	assert.Equal(t, "已有经理", meta.Manager)
	assert.Equal(t, "equity", meta.FundType)
	assert.Equal(t, "已有基准", meta.Benchmark)
	assert.Equal(t, "中高", meta.RiskLevel)
	assert.True(t, meta.LatestNAV.Equal(existingNAV))
	assert.Equal(t, 2025, meta.NAVDate.Year())
}

// TestEastmoneyFundDetailEnricher_FallbackName 验证主源没拿到名称时，
// jbgk 的"基金简称"作为兜底名称（核心场景：pingzhongdata 被反爬时仍能产出数据）。
func TestEastmoneyFundDetailEnricher_FallbackName(t *testing.T) {
	ts, _, _ := newFundDetailTestServer(t, fakeJbgkHTML, fakeFundgzJS)

	enricher := NewEastmoneyFundDetailEnricher(time.Second,
		WithFundJbgkBaseURL(ts.URL),
		WithFundBaseURL(ts.URL))
	meta := &AssetMeta{Source: "enricher_only"}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "011022",
	}, meta)
	require.NoError(t, err)

	assert.Equal(t, "汇添富互联网核心资产六个月持有混合C", meta.Name)
	assert.Equal(t, "汇添富基金", meta.Company)
	assert.Equal(t, "hybrid", meta.FundType)
}

// TestEastmoneyFundDetailEnricher_SkipNonFund 验证非基金资产直接跳过，不发请求。
func TestEastmoneyFundDetailEnricher_SkipNonFund(t *testing.T) {
	ts, jbgkCalls, gzCalls := newFundDetailTestServer(t, fakeJbgkHTML, fakeFundgzJS)

	enricher := NewEastmoneyFundDetailEnricher(time.Second,
		WithFundJbgkBaseURL(ts.URL),
		WithFundBaseURL(ts.URL))
	stockMeta := &AssetMeta{Name: "贵州茅台", Market: "SH"}
	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "stock", AssetCode: "600519", Market: "SH",
	}, stockMeta)
	require.NoError(t, err)
	assert.Equal(t, 0, *jbgkCalls)
	assert.Equal(t, 0, *gzCalls)
}

// TestEastmoneyFundDetailEnricher_SkipWhenAllFilled 验证全字段已填时不发请求（性能优化）。
func TestEastmoneyFundDetailEnricher_SkipWhenAllFilled(t *testing.T) {
	ts, jbgkCalls, gzCalls := newFundDetailTestServer(t, fakeJbgkHTML, fakeFundgzJS)

	enricher := NewEastmoneyFundDetailEnricher(time.Second,
		WithFundJbgkBaseURL(ts.URL),
		WithFundBaseURL(ts.URL))
	meta := &AssetMeta{
		Name:      "已填名称",
		Company:   "已填公司",
		Manager:   "已填经理",
		FundType:  "hybrid",
		Benchmark: "已填基准",
		LatestNAV: decimal.RequireFromString("1.0"),
		NAVDate:   time.Date(2026, 5, 22, 0, 0, 0, 0, time.Local),
	}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "011022",
	}, meta)
	require.NoError(t, err)
	assert.Equal(t, 0, *jbgkCalls, "全字段已填时不应发请求")
	assert.Equal(t, 0, *gzCalls, "全字段已填时不应发请求")
}

// TestEastmoneyFundDetailEnricher_NetworkErrorDoesNotPropagate 验证 jbgk 网络错误不返回 error。
func TestEastmoneyFundDetailEnricher_NetworkErrorDoesNotPropagate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	enricher := NewEastmoneyFundDetailEnricher(time.Second,
		WithFundJbgkBaseURL(ts.URL),
		WithFundBaseURL(ts.URL))
	meta := &AssetMeta{Name: "已有名称"}
	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "011022",
	}, meta)
	require.NoError(t, err, "jbgk/fundgz 失败不应返回 error，仅 graceful degrade")
	assert.Equal(t, "", meta.Company)
}

// TestEastmoneyFundDetailEnricher_NilMetaIsSafe 验证 nil meta 不 panic。
func TestEastmoneyFundDetailEnricher_NilMetaIsSafe(t *testing.T) {
	enricher := NewEastmoneyFundDetailEnricher(time.Second)
	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "011022",
	}, nil)
	require.NoError(t, err)
}

// TestEastmoneyFundDetailEnricher_NAVOnlyFromFundgz 验证当 jbgk 解析失败时，
// fundgz 仍能独立把净值/净值日期填上（双源解耦）。
func TestEastmoneyFundDetailEnricher_NAVOnlyFromFundgz(t *testing.T) {
	ts, _, gzCalls := newFundDetailTestServer(t, "<html>nothing useful</html>", fakeFundgzJS)

	enricher := NewEastmoneyFundDetailEnricher(time.Second,
		WithFundJbgkBaseURL(ts.URL),
		WithFundBaseURL(ts.URL))
	meta := &AssetMeta{Name: "已有名称"}

	err := enricher.Enrich(context.Background(), AssetKey{
		AssetType: "fund", AssetCode: "011022",
	}, meta)
	require.NoError(t, err)
	assert.True(t, meta.LatestNAV.Equal(decimal.RequireFromString("1.3742")))
	assert.Equal(t, 22, meta.NAVDate.Day())
	assert.Equal(t, 1, *gzCalls)
}

// TestMapFundRiskLabel 验证风险等级标签映射的稳定性。
func TestMapFundRiskLabel(t *testing.T) {
	cases := map[string]string{
		"低风险":  "低",
		"中低风险": "中低",
		"中风险":  "中",
		"中高风险": "中高",
		"高风险":  "高",
		"未知":   "未知",
		"":     "",
	}
	for in, want := range cases {
		assert.Equal(t, want, mapFundRiskLabel(in), "input=%q", in)
	}
}
