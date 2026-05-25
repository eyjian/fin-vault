package platformapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// =====================================================================
// EastmoneyFetcher
// =====================================================================
//
// 接入两个公开端点（无需 token）：
//   - 基金净值：{fundBaseURL}/js/{code}.js
//     默认 fundBaseURL = https://fundgz.1234567.com.cn
//     返回 jsonp： jsonpgz({"fundcode":"110022","name":"...","jzrq":"2026-05-15","dwjz":"2.6512","gsz":"2.6512","gszzl":"-0.32","gztime":"..."});
//   - 股票实时行情：{stockBaseURL}/api/qt/stock/get?secid=1.600519&fields=f43,f169,f170,...
//     默认 stockBaseURL = https://push2.eastmoney.com
//     返回 JSON： {"data":{"f43":172800,"f169":0,"f170":-15,"f57":"600519",...}}（f43 单位为分）
//
// 测试时用 WithFundBaseURL / WithStockBaseURL 注入 httptest 服务器地址，
// 即可覆盖 jsonp/JSON 字段解析路径而不依赖外网。

type eastmoneyFetcher struct {
	client       *resty.Client
	fundBaseURL  string
	stockBaseURL string
}

// NewEastmoneyFetcher 构造东方财富 Fetcher。
//
//	timeout<=0 时默认 5s；可选 With*BaseURL 注入测试地址。
func NewEastmoneyFetcher(timeout time.Duration, opts ...FetcherOption) QuoteFetcher {
	cfg := defaultConfig(timeout)
	for _, opt := range opts {
		opt(cfg)
	}
	c := resty.New().
		SetTimeout(cfg.timeout).
		SetHeader("User-Agent", "Mozilla/5.0 FinVault/1.0").
		SetHeader("Referer", "https://quote.eastmoney.com").
		SetRetryCount(1).
		SetRetryWaitTime(500 * time.Millisecond)
	return &eastmoneyFetcher{
		client:       c,
		fundBaseURL:  cfg.fundBaseURL,
		stockBaseURL: cfg.stockBaseURL,
	}
}

// Source 返回来源标识。
func (f *eastmoneyFetcher) Source() string { return "api_eastmoney" }

// Supports 东方财富支持基金 + 股票。
func (f *eastmoneyFetcher) Supports(a AssetKey) bool {
	return a.AssetType == "fund" || a.AssetType == "stock"
}

// FetchOne 拉取一条行情。
func (f *eastmoneyFetcher) FetchOne(ctx context.Context, a AssetKey) (*QuoteResult, error) {
	if !f.Supports(a) {
		return nil, ErrUnsupportedAsset
	}
	if a.AssetType == "fund" {
		return f.fetchFund(ctx, a)
	}
	return f.fetchStock(ctx, a)
}

// fetchFund 基金估值（实时净值）。
func (f *eastmoneyFetcher) fetchFund(ctx context.Context, a AssetKey) (*QuoteResult, error) {
	if a.AssetCode == "" {
		return nil, errors.New("eastmoney fund: empty asset code")
	}
	url := fmt.Sprintf("%s/js/%s.js", f.fundBaseURL, a.AssetCode)
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, fmt.Errorf("eastmoney fund http: %w", err)
	}
	body := strings.TrimSpace(resp.String())
	if body == "" {
		return nil, ErrNoData
	}
	// jsonpgz({...});
	start := strings.Index(body, "(")
	end := strings.LastIndex(body, ")")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("eastmoney fund: unexpected body: %q", truncate(body, 80))
	}
	jsonBody := body[start+1 : end]
	if jsonBody == "" {
		return nil, ErrNoData
	}
	priceStr := extractJSONString(jsonBody, "gsz")
	if priceStr == "" {
		priceStr = extractJSONString(jsonBody, "dwjz")
	}
	if priceStr == "" {
		return nil, fmt.Errorf("eastmoney fund: no price in body")
	}
	price, err := decimal.NewFromString(priceStr)
	if err != nil {
		return nil, fmt.Errorf("eastmoney fund: bad price %q: %w", priceStr, err)
	}
	changePct := decimal.Zero
	if s := extractJSONString(jsonBody, "gszzl"); s != "" {
		if v, err := decimal.NewFromString(s); err == nil {
			changePct = v
		}
	}
	qt := time.Now()
	if s := extractJSONString(jsonBody, "gztime"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local); err == nil {
			qt = t
		}
	}
	return &QuoteResult{
		AssetID:   a.AssetID,
		Price:     price,
		ChangePct: changePct,
		QuoteTime: qt,
		Source:    f.Source(),
		RawText:   truncate(body, 200),
	}, nil
}

// fetchStock 股票（A 股 / 港股 / 美股部分）。
//
// secid 规则：1=SH, 0=SZ, 116=HK, 105=US（美股需大写代码）。
func (f *eastmoneyFetcher) fetchStock(ctx context.Context, a AssetKey) (*QuoteResult, error) {
	if a.AssetCode == "" {
		return nil, errors.New("eastmoney stock: empty asset code")
	}
	prefix := mapEastmoneyMarket(a.Market)
	if prefix == "" {
		return nil, fmt.Errorf("%w: unknown market %q", ErrUnsupportedAsset, a.Market)
	}
	url := fmt.Sprintf("%s/api/qt/stock/get?secid=%s.%s&fields=f43,f44,f45,f46,f57,f58,f86,f168,f169,f170",
		f.stockBaseURL, prefix, a.AssetCode)
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, fmt.Errorf("eastmoney stock http: %w", err)
	}
	body := resp.String()
	if body == "" || !strings.Contains(body, `"data"`) {
		return nil, ErrNoData
	}
	priceStr := extractJSONNumber(body, "f43") // 当前价（单位：分）
	if priceStr == "" {
		return nil, fmt.Errorf("eastmoney stock: no f43 in body")
	}
	price, err := decimal.NewFromString(priceStr)
	if err != nil {
		return nil, fmt.Errorf("eastmoney stock: bad price %q: %w", priceStr, err)
	}
	// f43 单位是 分，除以 100 得到元
	price = price.Div(decimal.NewFromInt(100))

	changePct := decimal.Zero
	if s := extractJSONNumber(body, "f170"); s != "" {
		if v, err := decimal.NewFromString(s); err == nil {
			// f170 已经是百分比 * 100，再 ÷ 100
			changePct = v.Div(decimal.NewFromInt(100))
		}
	}
	volume := decimal.Zero
	if s := extractJSONNumber(body, "f86"); s != "" {
		if v, err := decimal.NewFromString(s); err == nil {
			volume = v
		}
	}

	return &QuoteResult{
		AssetID:   a.AssetID,
		Price:     price,
		ChangePct: changePct,
		Volume:    volume,
		QuoteTime: time.Now(),
		Source:    f.Source(),
		RawText:   truncate(body, 200),
	}, nil
}

// mapEastmoneyMarket A 股 / 港股 / 美股 的 secid 前缀。
func mapEastmoneyMarket(m string) string {
	switch strings.ToUpper(m) {
	case "SH":
		return "1"
	case "SZ", "BJ": // 北交所暂归 0 试探
		return "0"
	case "HK":
		return "116"
	case "US":
		return "105"
	}
	return ""
}
