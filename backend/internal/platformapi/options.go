package platformapi

import "time"

// =====================================================================
// 通用 Fetcher 选项（functional options pattern）
// =====================================================================
//
// 默认走真实第三方公开端点。测试时通过 With*BaseURL 注入 httptest 服务器地址，
// 单测既能覆盖 jsonp/CSV 字段解析逻辑，又不依赖外网。

// FetcherOption 用于在构造 Fetcher 时按需修改可选字段。
type FetcherOption func(*fetcherConfig)

// fetcherConfig 各 Fetcher 共享的可调参数。
type fetcherConfig struct {
	timeout time.Duration

	// eastmoney
	fundBaseURL       string // 默认 https://fundgz.1234567.com.cn
	fundDetailBaseURL string // 默认 https://fund.eastmoney.com（基金元信息：pingzhongdata）
	fundJJJBQKBaseURL string // 默认 https://api.fund.eastmoney.com（基金 JJJBQK：备用补全源；目前对很多 C 类基金返回 404，仅保留以便回退）
	fundJbgkBaseURL   string // 默认 https://fundf10.eastmoney.com（基金基本概况 HTML 页：jbgk_{code}.html，主用补全源）
	stockBaseURL      string // 默认 https://push2.eastmoney.com
	stockF10BaseURL   string // 默认 https://datacenter.eastmoney.com（股票 F10 基本资料：行业/板块/上市日）

	// sina
	sinaBaseURL string // 默认 https://hq.sinajs.cn

	// tencent
	tencentBaseURL string // 默认 https://qt.gtimg.cn
}

// defaultConfig 返回填好默认 base URL 的配置。
func defaultConfig(timeout time.Duration) *fetcherConfig {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &fetcherConfig{
		timeout:           timeout,
		fundBaseURL:       "https://fundgz.1234567.com.cn",
		fundDetailBaseURL: "https://fund.eastmoney.com",
		fundJJJBQKBaseURL: "https://api.fund.eastmoney.com",
		fundJbgkBaseURL:   "https://fundf10.eastmoney.com",
		stockBaseURL:      "https://push2.eastmoney.com",
		stockF10BaseURL:   "https://datacenter.eastmoney.com",
		sinaBaseURL:       "https://hq.sinajs.cn",
		tencentBaseURL:    "https://qt.gtimg.cn",
	}
}

// WithFundBaseURL 覆盖东方财富基金净值端点 baseURL（仅测试用）。
//
// 例如：WithFundBaseURL(httptestServer.URL) 后实际请求 {URL}/js/{code}.js。
func WithFundBaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.fundBaseURL = trimRightSlash(url)
		}
	}
}

// WithFundDetailBaseURL 覆盖东方财富基金详情端点 baseURL（仅测试用）。
//
// 默认指向 https://fund.eastmoney.com，资产元信息探测会请求
// {URL}/pingzhongdata/{code}.js 解析基金公司 / 经理 / 类型等字段。
func WithFundDetailBaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.fundDetailBaseURL = trimRightSlash(url)
		}
	}
}

// WithFundJJJBQKBaseURL 覆盖东方财富基金 JJJBQK（基本概况）端点 baseURL（仅测试用）。
//
// 默认指向 https://api.fund.eastmoney.com，基金补全器会请求
// {URL}/f10/JJJBQK?FCODE={code}… 解析基金公司/类型/业绩基准/风险等级/净值等字段
// （pingzhongdata 被反爬或字段缺失时的补充源）。
func WithFundJJJBQKBaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.fundJJJBQKBaseURL = trimRightSlash(url)
		}
	}
}

// WithFundJbgkBaseURL 覆盖东方财富基金基本概况 HTML 端点 baseURL（仅测试用）。
//
// 默认指向 https://fundf10.eastmoney.com，基金补全器会请求
// {URL}/jbgk_{code}.html 解析基金简称/类型/管理人/经理/业绩比较基准等字段，
// 这是替代不稳定 JJJBQK 接口的主用源。
func WithFundJbgkBaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.fundJbgkBaseURL = trimRightSlash(url)
		}
	}
}

// WithStockBaseURL 覆盖东方财富股票行情端点 baseURL（仅测试用）。
//
// 例如：WithStockBaseURL(httptestServer.URL) 后实际请求 {URL}/api/qt/stock/get?...。
func WithStockBaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.stockBaseURL = trimRightSlash(url)
		}
	}
}

// WithStockF10BaseURL 覆盖东方财富股票 F10 基本资料端点 baseURL（仅测试用）。
//
// 默认指向 https://datacenter.eastmoney.com，资产元信息探测会请求
// {URL}/securities/api/data/v1/get?... 解析行业/板块/上市日（push2 接口字段不稳定时的补充源）。
func WithStockF10BaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.stockF10BaseURL = trimRightSlash(url)
		}
	}
}

// WithSinaBaseURL 覆盖新浪股票端点 baseURL（仅测试用）。
//
// 例如：WithSinaBaseURL(httptestServer.URL) 后实际请求 {URL}/list=sh600519。
func WithSinaBaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.sinaBaseURL = trimRightSlash(url)
		}
	}
}

// WithTencentBaseURL 覆盖腾讯股票端点 baseURL（仅测试用）。
//
// 例如：WithTencentBaseURL(httptestServer.URL) 后实际请求 {URL}/q=sh600519。
func WithTencentBaseURL(url string) FetcherOption {
	return func(c *fetcherConfig) {
		if url != "" {
			c.tencentBaseURL = trimRightSlash(url)
		}
	}
}

// trimRightSlash 去掉末尾多余的斜杠。
func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
