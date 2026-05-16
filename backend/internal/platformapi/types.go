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
