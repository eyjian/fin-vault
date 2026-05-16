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
// SinaFetcher —— 新浪财经实时股票行情
// =====================================================================
//
// 端点：{sinaBaseURL}/list=sh600519,sz000001
// 默认 sinaBaseURL = https://hq.sinajs.cn
// 响应：var hq_str_sh600519="贵州茅台,1700.00,1701.00,1690.00,1720.00,1680.00,...";
// 字段含义（A 股）参考公开文档：
//   0 名称  1 今开  2 昨收  3 当前价  4 最高  5 最低  ... 8 成交量(股) 9 成交额(元)
// 港股：var hq_str_hk00700="腾讯控股,...,300.00,..."（字段顺序不同）。

type sinaFetcher struct {
	client  *resty.Client
	baseURL string
}

// NewSinaFetcher 构造新浪 Fetcher。
//
//	timeout<=0 时默认 5s；可选 WithSinaBaseURL 注入测试地址。
func NewSinaFetcher(timeout time.Duration, opts ...FetcherOption) QuoteFetcher {
	cfg := defaultConfig(timeout)
	for _, opt := range opts {
		opt(cfg)
	}
	c := resty.New().
		SetTimeout(cfg.timeout).
		SetHeader("User-Agent", "Mozilla/5.0 FinVault/1.0").
		SetHeader("Referer", "https://finance.sina.com.cn").
		SetRetryCount(1)
	return &sinaFetcher{client: c, baseURL: cfg.sinaBaseURL}
}

// Source 来源标识。
func (f *sinaFetcher) Source() string { return "api_sina" }

// Supports 仅股票。
func (f *sinaFetcher) Supports(a AssetKey) bool {
	if a.AssetType != "stock" {
		return false
	}
	switch strings.ToUpper(a.Market) {
	case "SH", "SZ", "HK", "BJ":
		return true
	}
	return false
}

// FetchOne 拉取一条行情。
func (f *sinaFetcher) FetchOne(ctx context.Context, a AssetKey) (*QuoteResult, error) {
	if !f.Supports(a) {
		return nil, ErrUnsupportedAsset
	}
	if a.AssetCode == "" {
		return nil, errors.New("sina: empty asset code")
	}
	prefix := strings.ToLower(a.Market)
	if a.Market == "BJ" {
		prefix = "bj"
	}
	url := fmt.Sprintf("%s/list=%s%s", f.baseURL, prefix, strings.ToLower(a.AssetCode))
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, fmt.Errorf("sina http: %w", err)
	}
	body := resp.String()
	if body == "" {
		return nil, ErrNoData
	}
	// var hq_str_sh600519="字段1,字段2,...";
	idx := strings.Index(body, `="`)
	end := strings.LastIndex(body, `"`)
	if idx < 0 || end <= idx+2 {
		return nil, fmt.Errorf("sina: unexpected body: %q", truncate(body, 80))
	}
	csv := body[idx+2 : end]
	fields := strings.Split(csv, ",")
	if len(fields) < 4 {
		return nil, ErrNoData
	}
	// A 股：fields[1]=今开 fields[2]=昨收 fields[3]=当前价 fields[8]=成交量
	// 港股：fields[6]=当前价（不同字段顺序），暂不深究做兜底：取第一个有意义的数字字段
	priceIdx := 3
	if strings.ToUpper(a.Market) == "HK" {
		priceIdx = 6
	}
	if priceIdx >= len(fields) {
		return nil, ErrNoData
	}
	priceStr := strings.TrimSpace(fields[priceIdx])
	price, err := decimal.NewFromString(priceStr)
	if err != nil {
		return nil, fmt.Errorf("sina: bad price %q: %w", priceStr, err)
	}
	changePct := decimal.Zero
	prevClose := decimal.Zero
	if len(fields) > 2 {
		if v, err := decimal.NewFromString(fields[2]); err == nil {
			prevClose = v
			if !prevClose.IsZero() {
				changePct = price.Sub(prevClose).Div(prevClose).Mul(decimal.NewFromInt(100)).Round(4)
			}
		}
	}
	volume := decimal.Zero
	if strings.ToUpper(a.Market) != "HK" && len(fields) > 8 {
		if v, err := decimal.NewFromString(fields[8]); err == nil {
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
