// FinVault 业务实体类型定义（与 docs/design/data-interfaces.yaml 对齐）。
// 所有金额/数量字段统一字符串传输（保精度），前端用 decimal.js 处理。

export type AssetType = 'fund' | 'stock' | 'wealth' | 'cash'
export type HoldingStatus = '持有中' | '已关闭' | '已到期'
export type AssetStatus = '活跃' | '已退市' | '已到期'
export type CostMethod = 'weighted_avg' | 'fifo'
export type TxnType =
  | 'buy'
  | 'sell'
  | 'dividend'
  | 'dividend_reinvest'
  | 'split'
  | 'bonus'
  | 'mature'
  | 'interest'
  | 'deposit'
  | 'withdraw'
  | 'cash_in'
  | 'cash_out'
  | 'adjust'
export type AIScene = 'chat' | 'buy_sell' | 'analysis' | 'advisor' | 'report'

export interface Platform {
  id: number
  code: string
  name: string
  platform_type: 'bank' | 'fund_platform' | 'broker' | 'internet'
  icon_url?: string
  status?: string
}

export interface FundDetail {
  asset_id?: number
  fund_type?: string
  manager?: string
  company?: string
  inception_date?: string
  latest_nav?: string
  latest_nav_date?: string
  benchmark?: string
}

export interface StockDetail {
  asset_id?: number
  market: 'SH' | 'SZ' | 'HK' | 'US' | 'BJ'
  industry?: string
  sector?: string
  listing_date?: string
  total_shares?: string
  latest_price?: string
  latest_price_time?: string
}

export interface WealthDetail {
  asset_id?: number
  product_type: 'fixed_deposit' | 'structured' | 'floating' | 'pension'
  expected_yield?: string
  actual_yield?: string
  start_date?: string
  end_date?: string
  term_days?: number
  min_amount?: string
  redemption_rule?: string
  is_auto_renewal?: boolean
}

// 单个资产的持仓汇总（与后端 domain.HoldingSummary 对齐）
export interface AssetHoldingSummary {
  quantity: string
  avg_cost: string
  total_cost: string
  realized_pnl: string
  total_dividend: string
  latest_price: string
  market_value: string
  unrealized_pnl: string
  total_pnl: string
  pnl_ratio: string
}

export interface Asset {
  id?: number
  user_id?: number
  asset_code: string
  name: string
  asset_type: AssetType
  currency: string
  issuer_platform_id?: number | null
  risk_level?: string | null
  status: AssetStatus
  remark?: string
  fund_detail?: FundDetail | null
  stock_detail?: StockDetail | null
  wealth_detail?: WealthDetail | null
  holding_summary?: AssetHoldingSummary | null
  created_at?: string
  updated_at?: string
}

// 组合层面的持仓汇总（与后端 service.HoldingSummary 对齐）
export interface HoldingSummary {
  display_currency: string
  total_market_value: string
  total_cost: string
  total_pnl: string
  pnl_ratio: string
  by_type: { asset_type: AssetType; market_value: string; ratio: string }[]
  by_platform: { platform_id: number; platform_name: string; market_value: string; ratio: string }[]
  by_currency: { currency: string; market_value: string; ratio: string }[]
}

export interface Holding {
  id: number
  user_id: number
  asset_id: number
  platform_id: number
  portfolio_id?: number | null
  quantity: string
  avg_cost: string
  total_cost: string
  realized_pnl: string
  total_dividend: string
  cost_method: CostMethod
  status: HoldingStatus
  first_buy_at?: string | null
  last_txn_at?: string | null
  asset?: Asset
}

export interface HoldingView extends Holding {
  latest_price?: string
  market_value?: string
  unrealized_pnl?: string
  total_pnl?: string
  pnl_ratio?: string
  market_value_cny?: string
}

export interface HoldingSummary {
  display_currency: string
  total_market_value: string
  total_cost: string
  total_pnl: string
  pnl_ratio: string
  by_type: { asset_type: AssetType; market_value: string; ratio: string }[]
  by_platform: { platform_id: number; platform_name: string; market_value: string; ratio: string }[]
  by_currency: { currency: string; market_value: string; ratio: string }[]
}

export interface Transaction {
  id?: number
  user_id?: number
  holding_id?: number
  asset_id?: number
  platform_id?: number
  txn_type: TxnType
  txn_time: string
  quantity: string
  price: string
  amount: string
  fee?: string
  tax?: string
  net_amount?: string
  currency: string
  source?: '手动' | '导入' | '自动到期'
  external_id?: string
  note?: string
  created_at?: string
  asset?: Asset
}

export interface PriceQuote {
  id?: number
  asset_id: number
  price: string
  quote_time?: string
  change_pct?: string
  volume?: string
  source?: '手动' | '新浪' | '腾讯' | '东方财富'
}

export interface ExchangeRate {
  id?: number
  from_currency: string
  to_currency: string
  rate: string
  quote_date?: string
  source?: '手动' | '央行' | 'API'
}

export interface AISession {
  id: string
  user_id: number
  title: string
  created_at: string
  updated_at: string
}

export interface AIMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  created_at: string
  token_usage?: Record<string, number>
}

export interface ProviderInfo {
  name: string
  model: string
  is_default: boolean
  enabled: boolean
}

export interface ToolCallDTO {
  name: string
  arguments?: Record<string, unknown>
  started_at: string
  finished_at: string
  status: 'success' | 'failed'
  error_message?: string
}

export interface TokenUsage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

export interface SendResp {
  assistant_message: AIMessage
  tool_calls: ToolCallDTO[]
  token_usage: TokenUsage
}
