package domain

// === 资产类型枚举 ===

// AssetType 资产大类。
type AssetType string

const (
	AssetTypeFund   AssetType = "fund"
	AssetTypeStock  AssetType = "stock"
	AssetTypeWealth AssetType = "wealth"
	AssetTypeCash   AssetType = "cash"
)

// IsValid 校验资产类型枚举值。
func (t AssetType) IsValid() bool {
	switch t {
	case AssetTypeFund, AssetTypeStock, AssetTypeWealth, AssetTypeCash:
		return true
	}
	return false
}

// === 交易类型枚举（13 种）===

// TxnType 交易类型，覆盖一阶段全部业务场景。
type TxnType string

const (
	TxnTypeBuy              TxnType = "buy"
	TxnTypeSell             TxnType = "sell"
	TxnTypeDividend         TxnType = "dividend"
	TxnTypeDividendReinvest TxnType = "dividend_reinvest"
	TxnTypeSplit            TxnType = "split"
	TxnTypeBonus            TxnType = "bonus"
	TxnTypeMature           TxnType = "mature"
	TxnTypeInterest         TxnType = "interest"
	TxnTypeDeposit          TxnType = "deposit"
	TxnTypeWithdraw         TxnType = "withdraw"
	TxnTypeCashIn           TxnType = "cash_in"
	TxnTypeCashOut          TxnType = "cash_out"
	TxnTypeAdjust           TxnType = "adjust"
)

// IsValid 校验交易类型。
func (t TxnType) IsValid() bool {
	switch t {
	case TxnTypeBuy, TxnTypeSell, TxnTypeDividend, TxnTypeDividendReinvest,
		TxnTypeSplit, TxnTypeBonus, TxnTypeMature, TxnTypeInterest,
		TxnTypeDeposit, TxnTypeWithdraw, TxnTypeCashIn, TxnTypeCashOut,
		TxnTypeAdjust:
		return true
	}
	return false
}

// === 持仓相关枚举 ===

// CostMethod 成本计算方法。
type CostMethod string

const (
	CostMethodWeightedAvg CostMethod = "weighted_avg"
	CostMethodFIFO        CostMethod = "fifo"
)

// HoldingStatus 持仓状态。
type HoldingStatus string

const (
	HoldingStatusHolding HoldingStatus = "持有中"
	HoldingStatusClosed  HoldingStatus = "已关闭"
	HoldingStatusMatured HoldingStatus = "已到期"
)

// === 通用状态枚举 ===

const (
	StatusActive    = "活跃"
	StatusInactive  = "停用"
	StatusDisabled  = "禁用"
	StatusDelisted  = "已退市"
	StatusMatured   = "已到期"
	StatusArchived  = "已归档"
	StatusDeleted   = "已删除"
)

// === 平台类型 ===

const (
	PlatformTypeBank          = "bank"
	PlatformTypeFundPlatform  = "fund_platform"
	PlatformTypeBroker        = "broker"
	PlatformTypeInternet      = "internet"
)

// === 交易来源 ===

const (
	TxnSourceManual      = "手动"
	TxnSourceImport      = "导入"
	TxnSourceAutoMature  = "自动到期"
)

// === 行情来源 ===

const (
	QuoteSourceManual    = "手动"
	QuoteSourceEastmoney = "东方财富"
	QuoteSourceSina      = "新浪"
	QuoteSourceTencent   = "腾讯"
)

// === 汇率来源 ===

const (
	RateSourceManual = "手动"
	RateSourcePBOC   = "央行"
	RateSourceAPI    = "API"
)
