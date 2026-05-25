// Package platformapi —— 东方财富 F10 股票元信息补全器（StockMetaEnricher 实现）
//
// 设计背景：
//
// 东方财富 push2 行情快照接口（push2.eastmoney.com/api/qt/stock/get）虽然在
// fields 参数中支持 f127（行业）/ f128（板块）/ f189（上市日），但对绝大多数 A 股
// 实际返回为空——该接口本质是行情快照，并不维护这些基本面字段。
//
// 真正可靠的来源是 datacenter F10 BASIC_ORGINFO 报表接口（datacenter.eastmoney.com），
// 它的 INDUSTRYCSRC1 / EM2016 / LISTING_DATE 字段对所有 A 股都有完整数据。
//
// 把这个补全做成独立的 StockMetaEnricher 而非塞进 EastmoneyMetaFetcher：
//   - 解耦：push2 网络故障（被反爬封 IP）时，主源会降级到新浪，但 F10 接口（不同域名/IP）
//     仍然可用，让降级路径也能享受行业/板块/上市日补全；
//   - 单一职责：fetcher 负责"拉主数据"，enricher 负责"补字段"，互不干扰；
//   - 易扩展：未来可以加更多 enricher（例如港股的不同基本面源），不影响主链路。
package platformapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// eastmoneyF10Enricher 调用东方财富 datacenter F10 BASIC_ORGINFO 报表，
// 为 A 股 meta 补全行业 / 板块 / 上市日字段。
//
// 端点：{f10BaseURL}/securities/api/data/v1/get?reportName=RPT_F10_BASIC_ORGINFO&...
// 默认 f10BaseURL = https://datacenter.eastmoney.com
//
// 测试时通过 WithStockF10BaseURL 注入 httptest 服务器地址。
type eastmoneyF10Enricher struct {
	client     *resty.Client
	f10BaseURL string
}

// NewEastmoneyF10Enricher 构造 F10 补全器。timeout<=0 时默认 5s。
func NewEastmoneyF10Enricher(timeout time.Duration, opts ...FetcherOption) MetaEnricher {
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
	return &eastmoneyF10Enricher{
		client:     c,
		f10BaseURL: cfg.stockF10BaseURL,
	}
}

// Enrich 按需补全 A 股 meta 的行业 / 板块 / 上市日。
//
// 仅在以下条件全部满足时才发请求，避免无谓的网络开销：
//   - asset_type == "stock"
//   - market 是 SH / SZ / BJ（A 股）
//   - meta 中至少有一个目标字段为空
//
// 网络错误、解析错误、字段缺失都 graceful degrade：返回 nil，主路径不受影响。
// 关键诊断信息通过 slog 输出，方便事后排查。
func (e *eastmoneyF10Enricher) Enrich(ctx context.Context, a AssetKey, meta *AssetMeta) error {
	if meta == nil || a.AssetType != "stock" {
		return nil
	}
	market := strings.ToUpper(a.Market)
	if !isAShareMarket(market) {
		return nil
	}
	if meta.Industry != "" && meta.Sector != "" && !meta.ListingDate.IsZero() {
		// 主源已经全部填好，跳过
		return nil
	}

	secucode := a.AssetCode + "." + market // 如 002190.SZ
	url := fmt.Sprintf("%s/securities/api/data/v1/get"+
		"?reportName=RPT_F10_BASIC_ORGINFO"+
		"&columns=SECUCODE,SECURITY_CODE,SECURITY_NAME_ABBR,INDUSTRYCSRC1,EM2016,LISTING_DATE"+
		`&filter=(SECUCODE=%%22%s%%22)`+
		"&pageNumber=1&pageSize=1", e.f10BaseURL, secucode)

	slog.Debug("eastmoney F10 request", slog.String("secucode", secucode), slog.String("url", url))

	resp, err := e.client.R().SetContext(ctx).Get(url)
	if err != nil {
		slog.Warn("eastmoney F10 http error", slog.String("secucode", secucode), slog.String("err", err.Error()))
		return nil
	}
	if resp.StatusCode() != 200 {
		slog.Warn("eastmoney F10 non-200",
			slog.String("secucode", secucode),
			slog.Int("status", resp.StatusCode()),
			slog.String("body_preview", truncate(resp.String(), 200)))
		return nil
	}
	body := resp.String()
	if body == "" || !strings.Contains(body, `"data"`) {
		slog.Warn("eastmoney F10 empty/no-data",
			slog.String("secucode", secucode),
			slog.String("body_preview", truncate(body, 200)))
		return nil
	}

	industryBefore := meta.Industry
	sectorBefore := meta.Sector
	listingBefore := meta.ListingDate

	if meta.Industry == "" {
		if v := extractJSONString(body, "INDUSTRYCSRC1"); v != "" {
			meta.Industry = ensureUTF8(v)
		}
	}
	if meta.Sector == "" {
		if v := extractJSONString(body, "EM2016"); v != "" {
			meta.Sector = ensureUTF8(v)
		}
	}
	if meta.ListingDate.IsZero() {
		if v := extractJSONString(body, "LISTING_DATE"); v != "" {
			// 样式 "2009-08-18 00:00:00" 或 "2009-08-18"
			layouts := []string{"2006-01-02 15:04:05", "2006-01-02"}
			for _, layout := range layouts {
				if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
					meta.ListingDate = t
					break
				}
			}
		}
	}

	slog.Info("eastmoney F10 enriched",
		slog.String("secucode", secucode),
		slog.String("industry_before", industryBefore),
		slog.String("industry_after", meta.Industry),
		slog.String("sector_before", sectorBefore),
		slog.String("sector_after", meta.Sector),
		slog.Bool("listing_filled", listingBefore.IsZero() && !meta.ListingDate.IsZero()))
	return nil
}
