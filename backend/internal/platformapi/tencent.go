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
// TencentFetcher —— 腾讯财经
// =====================================================================
//
// 端点：{tencentBaseURL}/q=sh600519
// 默认 tencentBaseURL = https://qt.gtimg.cn
// 响应：v_sh600519="1~贵州茅台~600519~1700.0~1690.0~..."；
// 字段中 [3]=当前价 [4]=昨收 [5]=今开 [6]=成交量(手)。

type tencentFetcher struct {
	client  *resty.Client
	baseURL string
}

// NewTencentFetcher 构造腾讯 Fetcher。
//
//	timeout<=0 默认 5s；可选 WithTencentBaseURL 注入测试地址。
func NewTencentFetcher(timeout time.Duration, opts ...FetcherOption) QuoteFetcher {
	cfg := defaultConfig(timeout)
	for _, opt := range opts {
		opt(cfg)
	}
	c := resty.New().
		SetTimeout(cfg.timeout).
		SetHeader("User-Agent", "Mozilla/5.0 FinVault/1.0").
		SetRetryCount(1)
	return &tencentFetcher{client: c, baseURL: cfg.tencentBaseURL}
}

// Source 来源。
func (f *tencentFetcher) Source() string { return "api_tencent" }

// Supports 仅 A 股 + 港股。
func (f *tencentFetcher) Supports(a AssetKey) bool {
	if a.AssetType != "stock" {
		return false
	}
	switch strings.ToUpper(a.Market) {
	case "SH", "SZ", "HK", "BJ":
		return true
	}
	return false
}

// FetchOne 抓取一条。
func (f *tencentFetcher) FetchOne(ctx context.Context, a AssetKey) (*QuoteResult, error) {
	if !f.Supports(a) {
		return nil, ErrUnsupportedAsset
	}
	if a.AssetCode == "" {
		return nil, errors.New("tencent: empty asset code")
	}
	// 市场前缀：sh/sz/hk/bj 与 strings.ToLower(a.Market) 一致
	prefix := strings.ToLower(a.Market)
	url := fmt.Sprintf("%s/q=%s%s", f.baseURL, prefix, strings.ToLower(a.AssetCode))
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, fmt.Errorf("tencent http: %w", err)
	}
	body := resp.String()
	if body == "" {
		return nil, ErrNoData
	}
	idx := strings.Index(body, `="`)
	end := strings.LastIndex(body, `"`)
	if idx < 0 || end <= idx+2 {
		return nil, fmt.Errorf("tencent: unexpected body: %q", truncate(body, 80))
	}
	parts := strings.Split(body[idx+2:end], "~")
	if len(parts) < 7 {
		return nil, ErrNoData
	}
	priceStr := strings.TrimSpace(parts[3])
	price, err := decimal.NewFromString(priceStr)
	if err != nil {
		return nil, fmt.Errorf("tencent: bad price %q: %w", priceStr, err)
	}
	changePct := decimal.Zero
	if v, err := decimal.NewFromString(parts[4]); err == nil && !v.IsZero() {
		changePct = price.Sub(v).Div(v).Mul(decimal.NewFromInt(100)).Round(4)
	}
	volume := decimal.Zero
	if v, err := decimal.NewFromString(parts[6]); err == nil {
		volume = v
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
