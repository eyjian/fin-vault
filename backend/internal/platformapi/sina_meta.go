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
// SinaMetaFetcher —— 新浪财经资产元信息探测（备用源）
// =====================================================================
//
// 作为东方财富 meta fetcher 的备用源，覆盖港股等东方财富不稳定的市场。
//
// 端点：{sinaBaseURL}/list={prefix}{code}
//   默认 sinaBaseURL = https://hq.sinajs.cn
//   与 SinaFetcher 复用同一端点，但只提取"名称 + 当前价"等元信息字段，
//   不关注涨跌幅/成交量等行情字段。
//
// 当前仅支持股票（港股 / A 股）；基金不支持（新浪无基金元信息端点）。

type sinaMetaFetcher struct {
	client  *resty.Client
	baseURL string
}

// NewSinaMetaFetcher 构造新浪元信息 Fetcher。
//
//	timeout<=0 时默认 5s；可选 WithSinaBaseURL 注入测试地址。
func NewSinaMetaFetcher(timeout time.Duration, opts ...FetcherOption) AssetMetaFetcher {
	cfg := defaultConfig(timeout)
	for _, opt := range opts {
		opt(cfg)
	}
	c := resty.New().
		SetTimeout(cfg.timeout).
		SetHeader("User-Agent", "Mozilla/5.0 FinVault/1.0").
		SetHeader("Referer", "https://finance.sina.com.cn").
		SetRetryCount(2).
		SetRetryWaitTime(800 * time.Millisecond)
	return &sinaMetaFetcher{
		client:  c,
		baseURL: cfg.sinaBaseURL,
	}
}

// Source 返回来源标识。
func (f *sinaMetaFetcher) Source() string { return "api_sina" }

// Supports 当前仅覆盖股票（A 股 / 港股）；基金不支持。
func (f *sinaMetaFetcher) Supports(a AssetKey) bool {
	if a.AssetType != "stock" {
		return false
	}
	switch strings.ToUpper(a.Market) {
	case "SH", "SZ", "HK", "BJ", "":
		return true
	}
	return false
}

// FetchMeta 拉取一条股票元信息。
//
// 新浪 hq 端点返回 CSV 格式，字段含义：
//
//	A 股：0=名称 1=今开 2=昨收 3=当前价 4=最高 5=最低 ...
//	港股：0=名称 1=今开 2=昨收 3=最高 4=最低 5=昨收 6=当前价 ...
//
// 本实现仅提取 Name 和 LatestPrice，其余字段留给东方财富主源。
func (f *sinaMetaFetcher) FetchMeta(ctx context.Context, a AssetKey) (*AssetMeta, error) {
	if !f.Supports(a) {
		return nil, ErrUnsupportedAsset
	}
	if a.AssetCode == "" {
		return nil, errors.New("sina meta: empty asset code")
	}

	prefix := strings.ToLower(a.Market)
	if a.Market == "BJ" {
		prefix = "bj"
	}
	if prefix == "" {
		// 未传 market 时尝试按代码前缀推断
		prefix = strings.ToLower(inferStockMarket(a.AssetCode))
	}
	if prefix == "" {
		return nil, fmt.Errorf("%w: cannot infer market for code %q", ErrUnsupportedAsset, a.AssetCode)
	}

	url := fmt.Sprintf("%s/list=%s%s", f.baseURL, prefix, strings.ToLower(a.AssetCode))
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, fmt.Errorf("sina meta http: %w", err)
	}
	body := resp.String()
	if body == "" {
		return nil, ErrNoData
	}

	// 解析 var hq_str_sh600519="字段1,字段2,...";
	idx := strings.Index(body, `="`)
	end := strings.LastIndex(body, `"`)
	if idx < 0 || end <= idx+2 {
		return nil, ErrNoData
	}
	csv := body[idx+2 : end]
	fields := strings.Split(csv, ",")
	if len(fields) < 4 {
		return nil, ErrNoData
	}

	name := strings.TrimSpace(fields[0])
	if name == "" {
		return nil, ErrNoData
	}

	meta := &AssetMeta{
		Name:   name,
		Source: f.Source(),
		Market: strings.ToUpper(a.Market),
	}

	// 当前价字段位置：A 股 fields[3]，港股 fields[6]
	priceIdx := 3
	if strings.ToUpper(a.Market) == "HK" {
		priceIdx = 6
	}
	if priceIdx < len(fields) {
		if priceStr := strings.TrimSpace(fields[priceIdx]); priceStr != "" {
			if v, err := decimal.NewFromString(priceStr); err == nil {
				meta.LatestPrice = v
			}
		}
	}

	return meta, nil
}
