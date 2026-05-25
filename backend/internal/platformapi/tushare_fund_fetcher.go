// Package platformapi —— Tushare Pro 基金净值 Fetcher（AssetMetaFetcher 实现）
//
// 设计背景：
//
// 东方财富 fundgz.1234567.com.cn 的基金估值数据有延迟（显示的是估值而非确认净值），
// 且 pingzhongdata 对部分基金（尤其 C 类、新基金）字段不全；jbgk HTML 和 JJJBQK 接口
// 也无法覆盖最新确认净值。
//
// Tushare Pro（https://tushare.pro）提供免费的 fund_nav 接口（需注册，赠送 200 积分，
// fund_nav 消耗 120 积分），返回准确的确认净值（含净值日期），可作为额外的基金净值
// 数据源，也可独立使用。
//
// 本 Fetcher 设计：
//   - 仅用于基金（asset_type=fund），股票不支持；
//   - 通过 Tushare Pro 的 fund_nav 接口获取基金最新净值和净值日期；
//   - 需要用户在配置中提供 Tushare API Token（通过 data_providers.tushare.token 配置）；
//   - Token 未配置或 Tushare 未启用时，本 Fetcher 不参与探测（Supports 返回 false）；
//   - 与主源（东方财富 pingzhongdata / jbgk）解耦，可并行使用。
//
// Tushare fund_nav 接口说明：
//   - 端点：POST {baseURL}/fund_nav
//   - 请求体：{"api_name": "fund_nav", "token": "...", "fields": "end_date,annual_nav,nav",
//     "ts_code": "011022.OF"}
//   - 响应：{"data": {"items": [["20260522", "1.4187", "1.4187"], ...]}}
//   - end_date 格式为 YYYYMMDD，nav 为单位净值
//
// 注意事项：
//   - Tushare 使用 ts_code 格式：基金代码 + ".OF"（如 011022.OF）；
//   - fund_nav 每次最多返回最近 3000 条记录，我们只取最新一条；
//   - Tushare 对免费用户有频率限制（每分钟最多 200 次请求），低频调用不受影响。

package platformapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// tushareFundFetcher 通过 Tushare Pro API 获取基金净值元信息。
type tushareFundFetcher struct {
	client  *resty.Client
	baseURL string
	token   string
}

// NewTushareFundFetcher 构造 Tushare 基金净值 Fetcher。
//
// timeout<=0 时默认 5s；token 为 Tushare API Token（必须非空）；
// 可选 WithTushareBaseURL 注入测试地址。
func NewTushareFundFetcher(timeout time.Duration, token string, opts ...FetcherOption) AssetMetaFetcher {
	cfg := defaultConfig(timeout)
	for _, opt := range opts {
		opt(cfg)
	}
	if token == "" {
		slog.Warn("tushare fund fetcher: no token provided, will be disabled")
		return &tushareFundFetcher{token: ""} // token 为空时 Supports 返回 false
	}
	c := resty.New().
		SetTimeout(cfg.timeout).
		SetHeader("User-Agent", "Mozilla/5.0 FinVault/1.0").
		SetHeader("Referer", "https://tushare.pro").
		SetRetryCount(2).
		SetRetryWaitTime(500 * time.Millisecond)
	return &tushareFundFetcher{
		client:  c,
		baseURL: cfg.tushareBaseURL,
		token:   token,
	}
}

// Source 返回来源标识。
func (f *tushareFundFetcher) Source() string { return "api_tushare" }

// Supports 仅在 token 非空且资产类型为 fund 时返回 true。
func (f *tushareFundFetcher) Supports(a AssetKey) bool {
	if f.token == "" {
		return false
	}
	return a.AssetType == "fund"
}

// tushareAPIRequest Tushare Pro API 请求体。
type tushareAPIRequest struct {
	APIName string `json:"api_name"`
	Token   string `json:"token"`
	Fields  string `json:"fields"`
	Params  string `json:"params,omitempty"` // JSON 编码的参数对象
}

// tushareAPIResponse Tushare Pro API 响应体。
type tushareAPIResponse struct {
	RequestID string         `json:"request_id"`
	Code      int            `json:"code"`
	Message   string         `json:"msg"`
	Data      tushareAPIData `json:"data"`
}

// tushareAPIData Tushare API 响应中的 data 字段。
type tushareAPIData struct {
	Fields []string   `json:"fields"`
	Items  [][]string `json:"items"`
}

// FetchMeta 通过 Tushare Pro fund_nav 接口拉取基金最新净值元信息。
//
// 返回的 AssetMeta 包含：Name（留空，由主源填充）、LatestNAV、NAVDate、Source。
// 其他字段（Company/Manager/FundType/Benchmark/RiskLevel）留空，由 enricher 补全。
func (f *tushareFundFetcher) FetchMeta(ctx context.Context, a AssetKey) (*AssetMeta, error) {
	if !f.Supports(a) {
		return nil, ErrUnsupportedAsset
	}
	if a.AssetCode == "" {
		slog.Error("tushare fund meta: empty asset code")
		return nil, fmt.Errorf("tushare fund meta: empty asset code")
	}

	// Tushare 基金代码格式：6 位代码 + ".OF"
	tsCode := a.AssetCode + ".OF"

	reqBody := tushareAPIRequest{
		APIName: "fund_nav",
		Token:   f.token,
		Fields:  "end_date,nav",
		Params:  fmt.Sprintf(`{"ts_code":"%s"}`, tsCode),
	}

	url := fmt.Sprintf("%s/fund_nav", f.baseURL)
	resp, err := f.client.R().
		SetContext(ctx).
		SetBody(reqBody).
		SetHeader("Content-Type", "application/json").
		Post(url)
	if err != nil {
		slog.Error("tushare fund nav http error", slog.String("code", a.AssetCode), slog.String("err", err.Error()))
		return nil, fmt.Errorf("tushare fund nav http: %w", err)
	}
	if resp.StatusCode() != 200 {
		slog.Error("tushare fund nav non-200", slog.String("code", a.AssetCode), slog.Int("status", resp.StatusCode()), slog.String("body", truncate(resp.String(), 200)))
		return nil, fmt.Errorf("tushare fund nav non-200: status=%d, body=%s",
			resp.StatusCode(), truncate(resp.String(), 200))
	}

	var tResp tushareAPIResponse
	if err := json.Unmarshal(resp.Body(), &tResp); err != nil {
		slog.Error("tushare fund nav json decode error", slog.String("code", a.AssetCode), slog.String("err", err.Error()))
		return nil, fmt.Errorf("tushare fund nav json decode: %w", err)
	}
	if tResp.Code != 0 {
		slog.Error("tushare fund nav api error", slog.String("code", a.AssetCode), slog.Int("code", tResp.Code), slog.String("msg", tResp.Message))
		return nil, fmt.Errorf("tushare fund nav api error: code=%d, msg=%s", tResp.Code, tResp.Message)
	}
	if len(tResp.Data.Items) == 0 {
		slog.Warn("tushare fund nav no data", slog.String("code", a.AssetCode))
		return nil, ErrNoData
	}

	// 取最新一条（items 按日期倒序，第一条即最新）
	latestItem := tResp.Data.Items[0]
	fields := tResp.Data.Fields

	endDateIdx := -1
	navIdx := -1
	for i, f := range fields {
		switch f {
		case "end_date":
			endDateIdx = i
		case "nav":
			navIdx = i
		}
	}
	if endDateIdx == -1 || navIdx == -1 {
		slog.Error("tushare fund nav: missing end_date or nav in fields", slog.String("code", a.AssetCode), slog.Any("fields", fields))
		return nil, fmt.Errorf("tushare fund nav: missing end_date or nav in fields")
	}
	if endDateIdx >= len(latestItem) || navIdx >= len(latestItem) {
		slog.Error("tushare fund nav: index out of range in items", slog.String("code", a.AssetCode))
		return nil, fmt.Errorf("tushare fund nav: index out of range in items")
	}

	endDateStr := strings.TrimSpace(latestItem[endDateIdx])
	navStr := strings.TrimSpace(latestItem[navIdx])

	if endDateStr == "" || navStr == "" {
		slog.Warn("tushare fund nav: empty end_date or nav", slog.String("code", a.AssetCode))
		return nil, ErrNoData
	}

	// 解析净值日期：end_date 格式为 YYYYMMDD
	navDate, err := time.ParseInLocation("20060102", endDateStr, time.Local)
	if err != nil {
		slog.Error("tushare fund nav: invalid end_date", slog.String("code", a.AssetCode), slog.String("end_date", endDateStr), slog.String("err", err.Error()))
		return nil, fmt.Errorf("tushare fund nav: invalid end_date %q: %w", endDateStr, err)
	}

	// 解析净值
	nav, err := decimal.NewFromString(navStr)
	if err != nil {
		slog.Error("tushare fund nav: invalid nav", slog.String("code", a.AssetCode), slog.String("nav", navStr), slog.String("err", err.Error()))
		return nil, fmt.Errorf("tushare fund nav: invalid nav %q: %w", navStr, err)
	}

	meta := &AssetMeta{
		Source:    f.Source(),
		LatestNAV: nav,
		NAVDate:   navDate,
	}

	slog.Info("tushare fund nav fetched",
		slog.String("code", a.AssetCode),
		slog.String("nav", nav.String()),
		slog.String("nav_date", navDate.Format("2006-01-02")))

	return meta, nil
}
