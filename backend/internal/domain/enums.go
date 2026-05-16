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
	HoldingStatusHolding HoldingStatus = "holding"
	HoldingStatusClosed  HoldingStatus = "closed"
	HoldingStatusMatured HoldingStatus = "matured"
)

// === 通用状态枚举 ===

const (
	StatusActive    = "active"
	StatusInactive  = "inactive"
	StatusDisabled  = "disabled"
	StatusDelisted  = "delisted"
	StatusMatured   = "matured"
	StatusArchived  = "archived"
	StatusDeleted   = "deleted"
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
	TxnSourceManual      = "manual"
	TxnSourceImport      = "import"
	TxnSourceAutoMature  = "auto_mature"
)

// === 行情来源 ===

const (
	QuoteSourceManual    = "manual"
	QuoteSourceEastmoney = "api_eastmoney"
	QuoteSourceSina      = "api_sina"
	QuoteSourceTencent   = "api_tencent"
)

// === 汇率来源 ===

const (
	RateSourceManual = "manual"
	RateSourcePBOC   = "pboc"
	RateSourceAPI    = "api"
)
