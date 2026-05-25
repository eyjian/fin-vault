package platformapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// =====================================================================
// EastmoneyMetaFetcher —— 东方财富资产元信息探测
// =====================================================================
//
// 用途：资产录入页"按代码自动填充"，与行情刷新链路解耦。
//
// 端点：
//   - 基金详情：{fundDetailBaseURL}/pingzhongdata/{code}.js
//     默认 fundDetailBaseURL = https://fund.eastmoney.com
//     该端点返回的是 JS 脚本（含 var 声明），用 regex 提取关键字段：
//       var fS_name = "易方达消费行业";
//       var fS_code = "110022";
//       var fund_Rate = "1.50";          // 申购费率（不解析）
//       var jjglr   = "易方达基金";       // 基金管理人（公司）
//       var Data_currentFundManager = [{ name: "萧楠", ... }];
//       // 类型在 fund_sourceRate 等字段中较难稳定提取，本期采用 latest_nav 端点配合，
//       // 类型字段允许缺失（graceful degrade）。
//   - 股票元信息：{stockBaseURL}/api/qt/stock/get?secid={prefix}.{code}&fields=...
//     与现有 EastmoneyFetcher.fetchStock 复用同一 baseURL，仅扩展 fields：
//       f43=当前价(分) f57=代码 f58=名称 f127=所属行业 f128=所属板块 f189=上市日(YYYYMMDD)
//
// 测试时通过 WithFundDetailBaseURL / WithStockBaseURL 注入 httptest 服务器地址。

type eastmoneyMetaFetcher struct {
	client            *resty.Client
	fundDetailBaseURL string
	stockBaseURL      string
	stockF10BaseURL   string
}

// NewEastmoneyMetaFetcher 构造资产元信息 Fetcher。
//
//	timeout<=0 时默认 5s；可选 With*BaseURL 注入测试地址。
func NewEastmoneyMetaFetcher(timeout time.Duration, opts ...FetcherOption) AssetMetaFetcher {
	cfg := defaultConfig(timeout)
	for _, opt := range opts {
		opt(cfg)
	}
	c := resty.New().
		SetTimeout(cfg.timeout).
		SetHeader("User-Agent", "Mozilla/5.0 FinVault/1.0").
		SetHeader("Referer", "https://quote.eastmoney.com").
		SetRetryCount(2).
		SetRetryWaitTime(800 * time.Millisecond)
	return &eastmoneyMetaFetcher{
		client:            c,
		fundDetailBaseURL: cfg.fundDetailBaseURL,
		stockBaseURL:      cfg.stockBaseURL,
		stockF10BaseURL:   cfg.stockF10BaseURL,
	}
}

// Source 返回来源标识。
func (f *eastmoneyMetaFetcher) Source() string { return "api_eastmoney" }

// Supports 当前仅覆盖 fund / stock；wealth / cash 不支持。
func (f *eastmoneyMetaFetcher) Supports(a AssetKey) bool {
	return a.AssetType == "fund" || a.AssetType == "stock"
}

// FetchMeta 拉取一条资产元信息。
func (f *eastmoneyMetaFetcher) FetchMeta(ctx context.Context, a AssetKey) (*AssetMeta, error) {
	if !f.Supports(a) {
		return nil, ErrUnsupportedAsset
	}
	if a.AssetCode == "" {
		return nil, errors.New("eastmoney meta: empty asset code")
	}
	if a.AssetType == "fund" {
		return f.fetchFundMeta(ctx, a)
	}
	return f.fetchStockMeta(ctx, a)
}

// =====================================================================
// 基金元信息
// =====================================================================

// fetchFundMeta 解析 pingzhongdata/{code}.js。
//
// 该脚本字段较多，本期只提取 4 个关键字段（基金名 / 公司 / 经理 / 类型），
// 任何单字段解析失败 → 跳过该字段（graceful degrade），不让整个探测失败。
// 仅在 fS_name 也提不到时，才认为远端无数据（多半是脚本变更或代码错误）。
func (f *eastmoneyMetaFetcher) fetchFundMeta(ctx context.Context, a AssetKey) (*AssetMeta, error) {
	url := fmt.Sprintf("%s/pingzhongdata/%s.js", f.fundDetailBaseURL, a.AssetCode)
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, fmt.Errorf("eastmoney fund meta http: %w", err)
	}
	body := resp.String()
	if strings.TrimSpace(body) == "" {
		return nil, ErrNoData
	}
	name := extractJSVarString(body, "fS_name")
	if name == "" {
		// 名字都拿不到，认为远端无数据
		return nil, ErrNoData
	}
	meta := &AssetMeta{
		Name:   ensureUTF8(name),
		Source: f.Source(),
	}
	if v := extractJSVarString(body, "jjglr"); v != "" {
		meta.Company = ensureUTF8(v)
	}
	if v := extractCurrentFundManagerName(body); v != "" {
		meta.Manager = ensureUTF8(v)
	}
	if v := extractFundType(body); v != "" {
		meta.FundType = v
	}
	if v := extractJSVarString(body, "fS_jjjz"); v != "" {
		// 部分页面提供历史净值数组，本期不解析；net值改由 EastmoneyFetcher 行情链路覆盖
		_ = v
	}
	return meta, nil
}

// extractJSVarString 提取 `var KEY = "VALUE";` 中的 VALUE。
//
// 仅适用于值不含未转义双引号的简单情形，pingzhongdata 中的 fS_name / jjglr 满足。
func extractJSVarString(body, key string) string {
	// 匹配：var key = "..."  或  var key="..."
	re := regexp.MustCompile(`var\s+` + regexp.QuoteMeta(key) + `\s*=\s*"([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// extractCurrentFundManagerName 从 Data_currentFundManager 数组中取首位经理姓名。
//
// 形如：var Data_currentFundManager = [{"id":"...","name":"萧楠","star":4,...}];
func extractCurrentFundManagerName(body string) string {
	re := regexp.MustCompile(`Data_currentFundManager\s*=\s*\[\s*\{[^}]*?"name"\s*:\s*"([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// extractFundType 优先从 var fund_sourceRate 临近的类型描述中提取，
// 否则从 stockCodes / Data_assetAllocation 等结构特征推断；不再细做，缺失即返回 ""。
//
// 当前实现：直接抓取脚本中常见的 var fS_jjlx_name 或 fund_type 字符串字段，
// 如果都不存在就返回 ""，让上层保留用户原值。
func extractFundType(body string) string {
	for _, key := range []string{"fS_jjlx", "fundtype", "Data_fundType"} {
		if v := extractJSVarString(body, key); v != "" {
			return mapFundTypeLabel(v)
		}
	}
	return ""
}

// mapFundTypeLabel 把东方财富类型描述映射到表单内部 fund_type 取值。
//
// 已覆盖最常见的 6 种；未识别值原样返回，给 UI 保留可读性（用户可手动改）。
func mapFundTypeLabel(label string) string {
	switch {
	case strings.Contains(label, "股票"):
		return "equity"
	case strings.Contains(label, "债券"):
		return "bond"
	case strings.Contains(label, "混合"):
		return "hybrid"
	case strings.Contains(label, "货币"):
		return "money"
	case strings.Contains(label, "指数"):
		return "index"
	case strings.Contains(label, "QDII"), strings.Contains(label, "qdii"):
		return "qdii"
	}
	return label
}

// =====================================================================
// 股票元信息
// =====================================================================

// fetchStockMeta 复用现有 stock/get 端点，扩展 fields。
//
// 与 EastmoneyFetcher.fetchStock 的差别：
//   - 多取 f57 / f58 / f127 / f128 / f189，分别填到 AssetCode / Name / Industry / Sector / ListingDate；
//   - Market 由代码前缀本地推断，避免依赖 secid 反查。
func (f *eastmoneyMetaFetcher) fetchStockMeta(ctx context.Context, a AssetKey) (*AssetMeta, error) {
	market := strings.ToUpper(a.Market)
	if market == "" {
		market = inferStockMarket(a.AssetCode)
	}
	prefix := mapEastmoneyMarket(market)
	if prefix == "" {
		return nil, fmt.Errorf("%w: unknown market %q", ErrUnsupportedAsset, a.Market)
	}
	url := fmt.Sprintf("%s/api/qt/stock/get?secid=%s.%s&fields=f43,f57,f58,f127,f128,f189",
		f.stockBaseURL, prefix, a.AssetCode)
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return nil, fmt.Errorf("eastmoney stock meta http: %w", err)
	}
	body := resp.String()
	if body == "" || !strings.Contains(body, `"data"`) {
		return nil, ErrNoData
	}
	name := extractJSONString(body, "f58")
	if name == "" {
		return nil, ErrNoData
	}
	// 防御性兜底：如果远端返回了非 UTF-8 字节（极少见但不可控），尝试按 GBK 解码
	name = ensureUTF8(name)
	meta := &AssetMeta{
		Name:   name,
		Source: f.Source(),
		Market: market,
	}
	if priceStr := extractJSONNumber(body, "f43"); priceStr != "" {
		if v, err := decimal.NewFromString(priceStr); err == nil {
			// f43 单位为分，与 fetchStock 一致
			meta.LatestPrice = v.Div(decimal.NewFromInt(100))
		}
	}
	if isAShareMarket(market) {
		// f127 / f128 仅在 A 股稳定，港美股略过
		if v := extractJSONString(body, "f127"); v != "" {
			meta.Industry = ensureUTF8(v)
		}
		if v := extractJSONString(body, "f128"); v != "" {
			meta.Sector = ensureUTF8(v)
		}
	}
	if s := extractJSONNumber(body, "f189"); s != "" && len(s) == 8 {
		// f189 形如 20010827（YYYYMMDD）
		if t, err := time.ParseInLocation("20060102", s, time.Local); err == nil {
			meta.ListingDate = t
		}
	}

	// push2 接口对 A 股的 f127/f128/f189 实际返回常为空（接口定位是行情快照、非基本面）。
	// 这里在 A 股缺失任一字段时调用 datacenter F10 接口补充，仅补空、不覆盖。
	// F10调用失败不影响主路径，以保证名称/价格、获取体验不受影响。
	if isAShareMarket(market) && (meta.Industry == "" || meta.Sector == "" || meta.ListingDate.IsZero()) {
		f.enrichStockMetaFromF10(ctx, a.AssetCode, market, meta)
	}
	return meta, nil
}

// enrichStockMetaFromF10 调用东方财富 datacenter F10 基本资料接口，补充行业/板块/上市日。
//
// 接口示例：
//
//	https://datacenter.eastmoney.com/securities/api/data/v1/get
//	  ?reportName=RPT_F10_BASIC_ORGINFO
//	  &columns=SECUCODE,SECURITY_CODE,SECURITY_NAME_ABBR,INDUSTRYCSRC1,EM2016,LISTING_DATE
//	  &filter=(SECUCODE="002190.SZ")
//	  &pageNumber=1&pageSize=1
//
// 返回表现为标准 JSON：{"result":{"data":[{"INDUSTRYCSRC1":"车辆装备","EM2016":"汽车零部件","LISTING_DATE":"2009-08-18 00:00:00"}]}}。
//
// 该函数仅“补空”：然后代码中已有值的字段不会被覆盖。所有异常（网络、解析、字段缺失）
// 均 graceful degrade：记录事后不招起件、不使主路径失败。
func (f *eastmoneyMetaFetcher) enrichStockMetaFromF10(ctx context.Context, code, market string, meta *AssetMeta) {
	secucode := code + "." + market // 如 002190.SZ
	url := fmt.Sprintf("%s/securities/api/data/v1/get"+
		"?reportName=RPT_F10_BASIC_ORGINFO"+
		"&columns=SECUCODE,SECURITY_CODE,SECURITY_NAME_ABBR,INDUSTRYCSRC1,EM2016,LISTING_DATE"+
		`&filter=(SECUCODE=%%22%s%%22)`+
		"&pageNumber=1&pageSize=1", f.stockF10BaseURL, secucode)
	resp, err := f.client.R().SetContext(ctx).Get(url)
	if err != nil || resp.StatusCode() != 200 {
		return
	}
	body := resp.String()
	if body == "" || !strings.Contains(body, `"data"`) {
		return
	}
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
}

// inferStockMarket 按 A 股代码前缀推断市场。
//
// 规则（与前端 inferMarket 保持一致）：
//   - 6 开头 → SH
//   - 0 / 3 开头且为 6 位 A 股代码 → SZ
//   - 8 / 4 开头 → BJ
//   - 5 位纯数字且不符合上述 A 股/北交所规则 → HK（港股）
//   - 其它返回 ""，由调用方决定如何处理（例如 US 必须用户显式提供）。
func inferStockMarket(code string) string {
	if len(code) == 0 {
		return ""
	}
	c := code[0]
	switch c {
	case '6':
		return "SH"
	case '0', '3':
		// 6 位 A 股代码（如 000001、300001）→ SZ；港股也是 0 开头但为 5 位
		if len(code) == 6 {
			return "SZ"
		}
	case '8', '4':
		return "BJ"
	}
	// 5 位纯数字且不符合 A 股/北交所规则 → 港股（与前端 inferMarket 一致）
	if len(code) == 5 && isAllDigits(code) {
		return "HK"
	}
	return ""
}

// isAllDigits 判断字符串是否全为数字。
func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isAShareMarket 当前 A 股市场（SH/SZ/BJ）才启用 industry/sector 字段。
func isAShareMarket(m string) bool {
	switch strings.ToUpper(m) {
	case "SH", "SZ", "BJ":
		return true
	}
	return false
}
