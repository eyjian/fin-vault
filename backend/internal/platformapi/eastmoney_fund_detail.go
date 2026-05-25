// Package platformapi —— 东方财富基金详情补全器（MetaEnricher 实现）
//
// 设计背景：
//
// 主源 pingzhongdata/{code}.js 对部分基金（尤其 C 类、新基金，如 011022 汇添富互联网核心
// 资产六个月持有混合C）会缺失 jjglr/类型等字段；早期备援源 api.fund.eastmoney.com 的
// JJJBQK JSON 接口对相当一部分基金返回 ErrCode=4/404，无法稳定使用。
//
// 实测稳定可用的两个公开端点：
//
//  1. https://fundf10.eastmoney.com/jbgk_{code}.html
//     基金"基本概况"页面（HTML，UTF-8），包含：
//     - 基金简称、基金全称
//     - 基金类型（"混合型-偏股" 等）
//     - 基金管理人（公司）
//     - 基金经理人
//     - 业绩比较基准
//
//  2. https://fundgz.1234567.com.cn/js/{code}.js
//     基金估值 jsonp，包含 dwjz（最新单位净值）、jzrq（净值日期），对所有公募基金都可用。
//
// 二者组合即可覆盖表单需要的全部字段（风险等级 jbgk 页面没有，对前端可接受为空）。
//
// 本 enricher 设计：
//   - 主调 jbgk HTML，正则提取基金简称/类型/公司/经理/业绩基准；
//   - 净值/净值日期由独立的 fundgz JS 兜底（即使 jbgk 失败也能拿到这两项）；
//   - 仅"补空"，绝不覆盖主源 pingzhongdata 已有的值；
//   - 任意环节失败都 graceful degrade，不阻塞主探测路径。
package platformapi

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// eastmoneyFundDetailEnricher 调用东方财富 jbgk HTML + fundgz JS，
// 为基金 meta 补全名称 / 公司 / 经理 / 类型 / 业绩基准 / 最新净值 / 净值日期等字段。
type eastmoneyFundDetailEnricher struct {
	client      *resty.Client
	jbgkBaseURL string
	gzBaseURL   string
}

// NewEastmoneyFundDetailEnricher 构造基金详情补全器。timeout<=0 时默认 5s。
func NewEastmoneyFundDetailEnricher(timeout time.Duration, opts ...FetcherOption) MetaEnricher {
	cfg := defaultConfig(timeout)
	for _, opt := range opts {
		opt(cfg)
	}
	c := resty.New().
		SetTimeout(cfg.timeout).
		SetHeader("User-Agent", "Mozilla/5.0 FinVault/1.0").
		SetHeader("Referer", "https://fundf10.eastmoney.com").
		SetRetryCount(1).
		SetRetryWaitTime(500 * time.Millisecond)
	return &eastmoneyFundDetailEnricher{
		client:      c,
		jbgkBaseURL: cfg.fundJbgkBaseURL,
		gzBaseURL:   cfg.fundBaseURL,
	}
}

// Enrich 按需补全基金 meta。
//
// 仅对 asset_type=fund 生效；非基金资产直接返回。
// 如果 Name/Company/Manager/FundType/Benchmark/LatestNAV 全部已填好，跳过 HTTP 调用。
//
// 错误（网络、解析、字段缺失）一律 graceful degrade：返回 nil，主路径不受影响。
func (e *eastmoneyFundDetailEnricher) Enrich(ctx context.Context, a AssetKey, meta *AssetMeta) error {
	if meta == nil || a.AssetType != "fund" {
		return nil
	}
	// 全部填齐才能跳过；风险等级 jbgk 页面没有，不参与判定（避免每次都触发请求）
	if meta.Name != "" && meta.Company != "" && meta.Manager != "" &&
		meta.FundType != "" && meta.Benchmark != "" &&
		!meta.LatestNAV.IsZero() && !meta.NAVDate.IsZero() {
		return nil
	}

	e.enrichFromJbgk(ctx, a.AssetCode, meta)
	e.enrichNAVFromFundgz(ctx, a.AssetCode, meta)
	return nil
}

// enrichFromJbgk 解析 fundf10.eastmoney.com/jbgk_{code}.html 基本概况页。
//
// 该页中关键字段以 <th>名称</th><td>值</td> 形式出现在 <table class="info">，
// 用宽松的正则（不依赖严格 class）逐对抽取，避免页面样式微调时解析失败。
func (e *eastmoneyFundDetailEnricher) enrichFromJbgk(ctx context.Context, code string, meta *AssetMeta) {
	url := fmt.Sprintf("%s/jbgk_%s.html", e.jbgkBaseURL, code)
	slog.Debug("eastmoney jbgk request", slog.String("code", code), slog.String("url", url))

	resp, err := e.client.R().SetContext(ctx).Get(url)
	if err != nil {
		slog.Warn("eastmoney jbgk http error", slog.String("code", code), slog.String("err", err.Error()))
		return
	}
	if resp.StatusCode() != 200 {
		slog.Warn("eastmoney jbgk non-200",
			slog.String("code", code),
			slog.Int("status", resp.StatusCode()),
			slog.String("body_preview", truncate(resp.String(), 200)))
		return
	}
	body := resp.String()
	if body == "" {
		return
	}

	pairs := parseJbgkInfoTable(body)
	if len(pairs) == 0 {
		slog.Warn("eastmoney jbgk no info pairs",
			slog.String("code", code),
			slog.String("body_preview", truncate(body, 200)))
		return
	}

	// 把抽取到的中文键值对映射到 meta 字段（仅补空）
	for k, v := range pairs {
		v = strings.TrimSpace(stripHTMLTags(v))
		if v == "" {
			continue
		}
		switch {
		case strings.Contains(k, "基金简称"):
			if meta.Name == "" {
				meta.Name = v
			}
		case strings.Contains(k, "基金管理人"):
			if meta.Company == "" {
				meta.Company = v
			}
		case strings.Contains(k, "基金经理"): // "基金经理人"
			if meta.Manager == "" {
				meta.Manager = v
			}
		case strings.Contains(k, "基金类型"):
			if meta.FundType == "" {
				meta.FundType = mapFundTypeLabel(v)
			}
		case strings.Contains(k, "业绩比较基准"):
			if meta.Benchmark == "" {
				meta.Benchmark = v
			}
		}
	}

	// jbgk 页"基金代码"那一行实际是 `<th>基金代码</th><td>011022（前端）基金类型混合型-偏股</td>`，
	// "基金类型"嵌在同一个 td 里、不是独立的 th，标准 th/td 抽取拿不到。
	// 这里 fallback：对"基金代码"行剥标签后的纯文本，按"基金类型"截取后续内容。
	if meta.FundType == "" {
		for k, v := range pairs {
			if !strings.Contains(k, "基金代码") {
				continue
			}
			plain := stripHTMLTags(v)
			if idx := strings.Index(plain, "基金类型"); idx >= 0 {
				rest := strings.TrimSpace(plain[idx+len("基金类型"):])
				if rest != "" {
					meta.FundType = mapFundTypeLabel(rest)
				}
			}
			break
		}
	}

	slog.Info("eastmoney jbgk enriched",
		slog.String("code", code),
		slog.String("name", meta.Name),
		slog.String("company", meta.Company),
		slog.String("manager", meta.Manager),
		slog.String("fund_type", meta.FundType),
		slog.Bool("benchmark_filled", meta.Benchmark != ""))
}

// enrichNAVFromFundgz 用 fundgz.1234567.com.cn/js/{code}.js 兜底净值/净值日期。
//
// 返回 jsonp：jsonpgz({"fundcode":"011022","name":"...","jzrq":"2026-05-22","dwjz":"1.3742",...});
// 该端点对所有公募基金都可用，是补全净值最稳定的途径之一。
func (e *eastmoneyFundDetailEnricher) enrichNAVFromFundgz(ctx context.Context, code string, meta *AssetMeta) {
	if !meta.LatestNAV.IsZero() && !meta.NAVDate.IsZero() && meta.Name != "" {
		return
	}

	url := fmt.Sprintf("%s/js/%s.js", e.gzBaseURL, code)
	resp, err := e.client.R().SetContext(ctx).Get(url)
	if err != nil {
		slog.Warn("eastmoney fundgz http error", slog.String("code", code), slog.String("err", err.Error()))
		return
	}
	if resp.StatusCode() != 200 {
		slog.Warn("eastmoney fundgz non-200",
			slog.String("code", code),
			slog.Int("status", resp.StatusCode()))
		return
	}
	body := resp.String()
	if !strings.Contains(body, `"dwjz"`) {
		// 偶发返回 jsonpgz();（节假日刚开盘等情况），不算错
		return
	}

	if meta.Name == "" {
		if v := extractJSONString(body, "name"); v != "" {
			meta.Name = ensureUTF8(v)
		}
	}
	if meta.LatestNAV.IsZero() {
		if v := extractJSONString(body, "dwjz"); v != "" {
			if d, err := decimal.NewFromString(strings.TrimSpace(v)); err == nil {
				meta.LatestNAV = d
			}
		}
	}
	if meta.NAVDate.IsZero() {
		if v := extractJSONString(body, "jzrq"); v != "" {
			if t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(v), time.Local); err == nil {
				meta.NAVDate = t
			}
		}
	}

	slog.Info("eastmoney fundgz enriched",
		slog.String("code", code),
		slog.Bool("nav_filled", !meta.LatestNAV.IsZero()),
		slog.Bool("nav_date_filled", !meta.NAVDate.IsZero()))
}

// jbgkPairRe 匹配 <th>...</th>...<td>...</td> 一对（中间允许任意空白/属性）。
//
// 注意 jbgk 页面中"基金代码"和"基金类型"在同一个 td 内并排（th 用 colspan=4 或两组 th/td），
// 我们只关心 5 个目标字段，本正则按"上一对 th/td"贪婪匹配即可，少数行的复合结构会被
// 忽略（只要目标字段所在的标准行能匹配上）。
var jbgkPairRe = regexp.MustCompile(`(?s)<th[^>]*>(.*?)</th>\s*<td[^>]*>(.*?)</td>`)

// jbgkInfoTableRe 匹配 jbgk 页里 <table class="info"> ... </table> 的整段内容。
// class 名包含 "info" 即可（不要求严格 ="info"），避免对方调整样式时失效。
var jbgkInfoTableRe = regexp.MustCompile(`(?s)<table[^>]*class="[^"]*\binfo\b[^"]*"[^>]*>(.*?)</table>`)

// parseJbgkInfoTable 从 jbgk HTML 文本中抽取 info 表里所有 <th>键</th><td>值</td> 对。
//
// 返回 map[键中文]值 HTML（值仍保留 HTML，调用方再 stripHTMLTags）。
func parseJbgkInfoTable(html string) map[string]string {
	m := jbgkInfoTableRe.FindStringSubmatch(html)
	if len(m) < 2 {
		// 退而求其次：在整个文档里抓所有 th/td（仍能拿到目标字段）
		out := map[string]string{}
		for _, p := range jbgkPairRe.FindAllStringSubmatch(html, -1) {
			k := strings.TrimSpace(stripHTMLTags(p[1]))
			if k != "" {
				if _, exists := out[k]; !exists {
					out[k] = p[2]
				}
			}
		}
		return out
	}
	out := map[string]string{}
	for _, p := range jbgkPairRe.FindAllStringSubmatch(m[1], -1) {
		k := strings.TrimSpace(stripHTMLTags(p[1]))
		if k != "" {
			if _, exists := out[k]; !exists {
				out[k] = p[2]
			}
		}
	}
	return out
}

// stripHTMLTagsRe 去掉所有 HTML 标签。
var stripHTMLTagsRe = regexp.MustCompile(`<[^>]+>`)

// stripHTMLTags 去掉所有 HTML 标签并把多个空白合并为一个空格。
func stripHTMLTags(s string) string {
	s = stripHTMLTagsRe.ReplaceAllString(s, "")
	// 合并空白
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// mapFundRiskLabel 把"中风险"等中文描述映射到表单的标准值。
//
// 表单选项一般为：低 / 中低 / 中 / 中高 / 高（前端 risk_level select）。
// 已覆盖最常见的几种；未识别值原样返回，给 UI 保留可读性。
//
// （当前主源 jbgk 页不直接返回风险字段；此函数保留以备其他源调用，
// 也保留旧测试 TestMapFundRiskLabel 的兼容性。）
func mapFundRiskLabel(label string) string {
	s := strings.TrimSpace(label)
	switch {
	case strings.Contains(s, "中低"):
		return "中低"
	case strings.Contains(s, "中高"):
		return "中高"
	case strings.HasPrefix(s, "低"):
		return "低"
	case strings.HasPrefix(s, "高"):
		return "高"
	case strings.Contains(s, "中"):
		return "中"
	}
	return s
}
