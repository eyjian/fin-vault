// Package platformapi 提供第三方行情 / 汇率适配。
//
// 设计原则：
//   - 仅本包 import resty，service 层只依赖本包暴露的 Fetcher 接口；
//   - 每家平台一个文件实现（eastmoney / sina / tencent / pboc），通过工厂注册；
//   - 失败时返回 wrap error，service 层按 source_priority 自行降级。
package platformapi

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// === 错误 ===

// ErrUnsupportedAsset 当前 Provider 不支持给定的资产类型/市场。
var ErrUnsupportedAsset = errors.New("platformapi: unsupported asset for this provider")

// ErrNoData 远端无数据返回。
var ErrNoData = errors.New("platformapi: no data")

// === 数据模型 ===

// AssetKey 行情查询所需的最小资产标识（避免拉 domain.Asset 全字段）。
type AssetKey struct {
	AssetID   uint
	AssetType string // fund / stock / wealth / cash
	AssetCode string // 基金代码 / 股票代码（如 600519）
	Market    string // 股票专属：SH / SZ / HK / US / BJ
}

// QuoteResult 单个资产抓取结果。
type QuoteResult struct {
	AssetID   uint
	Price     decimal.Decimal
	ChangePct decimal.Decimal
	Volume    decimal.Decimal
	QuoteTime time.Time
	Source    string // "api_eastmoney" / "api_sina" / "api_tencent" / "api_pboc"
	RawText   string // 调试用：原始返回片段
	Err       error  // 单条失败也返回，调用方循环检查
}

// === 行情 Fetcher 接口 ===

// QuoteFetcher 单个 Provider 的行情拉取能力。
type QuoteFetcher interface {
	// Source 标识来源，写入 PriceQuote.Source。
	Source() string
	// Supports 返回是否支持给定资产。eastmoney 支持 fund/stock，sina/tencent 仅支持 stock。
	Supports(a AssetKey) bool
	// FetchOne 抓取单个资产行情。
	FetchOne(ctx context.Context, a AssetKey) (*QuoteResult, error)
}

// === 资产元信息 Fetcher（用于录入页"按代码自动填充"）===

// AssetMeta 资产可公开获取的元信息探测结果。
//
// 设计原则：
//   - decimal/time 字段使用 zero value 表示缺失（解析侧自行判断 IsZero）；
//   - 字符串字段空串表示缺失；
//   - 同一结构体复用于基金 / 股票，按 AssetType 分别填充对应字段，互不冲突。
type AssetMeta struct {
	// 通用
	Name   string // 资产名称（基金 / 股票名）
	Source string // 数据来源，例如 "api_eastmoney"

	// 基金专属
	Company   string          // 基金公司
	Manager   string          // 基金经理（多人时取首位 / 拼接，由实现决定）
	FundType  string          // equity / bond / hybrid / money / index / qdii
	LatestNAV decimal.Decimal // 最新单位净值
	NAVDate   time.Time       // 净值日期
	Benchmark string          // 业绩基准（如 "沪深300指数收益率×80%+中债综合指数收益率×20%"）
	RiskLevel string          // 风险等级（"低" / "中低" / "中" / "中高" / "高"），由实现自行映射

	// 股票专属
	Market      string          // 推断后的市场：SH / SZ / BJ（HK/US 不做推断）
	Industry    string          // 所属行业（A 股 f127）
	Sector      string          // 所属板块（A 股 f128）
	ListingDate time.Time       // 上市日期（A 股 f189，YYYYMMDD）
	LatestPrice decimal.Decimal // 当前价（已换算到元）
}

// AssetMetaFetcher 资产元信息探测能力。
//
// 与 QuoteFetcher 的差别：
//   - QuoteFetcher 关注价格/涨跌幅/成交量等"行情快照"，被 QuoteAggregator 在"批量刷新行情"
//     场景高频调用；
//   - AssetMetaFetcher 关注名称/公司/经理/行业/板块/上市日等"元信息"，仅被资产录入表单
//     "按代码探测"低频调用，独立链路、独立端点（基金详情 / 股票扩展字段）。
type AssetMetaFetcher interface {
	// Source 标识来源（与 QuoteFetcher 一致风格）。
	Source() string
	// Supports 返回是否支持给定资产。元信息探测当前仅覆盖 fund / stock。
	Supports(a AssetKey) bool
	// FetchMeta 拉取一条元信息。失败时返回 wrap error；
	// 远端无数据返回 ErrNoData；不支持的 AssetType/Market 返回 ErrUnsupportedAsset。
	FetchMeta(ctx context.Context, a AssetKey) (*AssetMeta, error)
}

// MetaEnricher 资产元信息"补全器"。
//
// 与 AssetMetaFetcher 的差别：
//   - AssetMetaFetcher 是主源，独立返回完整 *AssetMeta；
//   - MetaEnricher 不能独立产出 meta，仅在主源已成功返回 meta 后，
//     按"仅补空、不覆盖"策略填充缺失字段。
//
// 典型用例：
//   - 股票场景：东方财富 push2 行情快照接口对绝大多数 A 股不返回 f127/f128/f189，
//     用 datacenter F10 BASIC_ORGINFO 补行业/板块/上市日；
//   - 基金场景：fund.eastmoney.com 的 pingzhongdata 接口经常拿不到基金公司/业绩基准/
//     风险等级，用 api.fund.eastmoney.com 的 JJJBQK 接口补。
//
// 把这些做成 enricher 而非塞进主 fetcher，主要好处：
//   - 主源（含 push2 / pingzhongdata）失败被反爬封 IP 时，service 会降级到备用源（新浪等），
//     enricher 走的是不同域名/端点，不会同时挂掉，让降级路径也能享受字段补全；
//   - 单一职责：fetcher 负责"拉主数据"，enricher 负责"补字段"，互不干扰；
//   - 易扩展：未来可以再加 enricher 不影响主链路。
//
// 实现要求：
//   - Enrich 必须 graceful degrade：网络/解析失败不返回 error，仅静默跳过；
//   - 仅在 meta 中目标字段为空时填充，已有值不覆盖；
//   - 实现内部应自带超时控制，避免拖慢主路径；
//   - 实现可按 a.AssetType 自行判断是否处理（不处理时直接 return nil）。
type MetaEnricher interface {
	// Enrich 按需补全 meta 中的字段。返回 error 仅用于诊断日志，调用方会忽略。
	Enrich(ctx context.Context, a AssetKey, meta *AssetMeta) error
}

// StockMetaEnricher 是 MetaEnricher 的别名，保留以维持二进制 / 调用方兼容。
//
// Deprecated: 使用 MetaEnricher。基金类的 enricher 共享同一接口契约。
type StockMetaEnricher = MetaEnricher
