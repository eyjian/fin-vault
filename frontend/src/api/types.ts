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

// =====================================================================
// AI 把脉（spec ai-pulse-diagnosis）
// =====================================================================

// 把脉建议（与后端 domain.PulseRecommendation 对齐）：
//   - sell   建议卖出
//   - reduce 建议减仓
//   - hold   继续持有
//   - add    建议加仓
export type PulseRecommendation = 'sell' | 'reduce' | 'hold' | 'add'

// 把脉置信度（与后端 domain.PulseConfidence 对齐）：
//   - high   数据充分、信号明确
//   - medium 数据较完整但存在不确定性
//   - low    数据不足或市场信号矛盾，UI 显示"请谨慎参考"提示
export type PulseConfidence = 'high' | 'medium' | 'low'

// 把脉触发方式（与后端 domain.PulseTriggerSource 对齐）：
//   - manual    资产管理页面手动触发
//   - chat      AI 对话中工具调用触发
//   - scheduled 定时任务触发（未来扩展）
export type PulseTriggerSource = 'manual' | 'chat' | 'scheduled'

// 单个资产的把脉结果（与后端 handler.PulseDiagnoseItemDTO 对齐）。
// status：success 表示把脉成功（含数据不足兜底）；failed 表示底层错误（资产不存在 / LLM 异常等）。
export interface PulseDiagnosisResult {
  asset_id: number
  recommendation?: PulseRecommendation
  confidence?: PulseConfidence
  summary?: string
  detail?: string
  session_id?: string
  trigger_source?: PulseTriggerSource
  diagnosed_at?: string // RFC3339；前端用它计算"距今 N 天"
  status: 'success' | 'failed'
  error_message?: string
}

// POST /api/v1/ai/pulse-diagnosis 与 GET 接口的统一响应体。
export interface PulseDiagnosisResp {
  items: PulseDiagnosisResult[]
}

// =====================================================================
// 资产录入"按代码自动填充"（asset-form-autofill）
// =====================================================================

// GET /api/v1/assets/probe 响应：可公开获取的资产元信息。
//
// 字段约定：
//   - 后端使用 omitempty 序列化，除 source 外所有字段都是可选的；
//   - 数值字段（latest_nav / latest_price）在响应里是字符串，与系统的 decimal 字符串约定一致；
//   - 日期字段（nav_date / listing_date）格式 YYYY-MM-DD。
//
// 前端"仅填空"策略要点：把 result 中非空值写入 form 中"为空"的对应字段，
// 保留用户已填内容；详见 FundManage.vue / StockManage.vue。
export interface AssetProbeResult {
  source: string
  name?: string
  // fund-only
  company?: string
  manager?: string
  fund_type?: string
  latest_nav?: string
  nav_date?: string
  benchmark?: string
  risk_level?: string
  // stock-only
  market?: 'SH' | 'SZ' | 'HK' | 'US' | 'BJ'
  industry?: string
  sector?: string
  listing_date?: string
  latest_price?: string
}

export interface AssetProbeParams {
  asset_type: 'fund' | 'stock'
  asset_code: string
  market?: 'SH' | 'SZ' | 'HK' | 'US' | 'BJ'
}

// =====================================================================
// 多 API 服务商配置（基金净值数据源等）
// =====================================================================

export interface TushareConfig {
  enabled: boolean
  token: string
  base_url: string
}

export interface DataProvidersConfig {
  tushare: TushareConfig
}

// =====================================================================
// AI 服务商配置（DeepSeek / OpenAI 等）
// =====================================================================

export interface AIProviderConfig {
  name: string       // 服务商标识：deepseek / openai / ...
  enabled: boolean   // 是否启用
  api_key: string    // API Key（脱敏显示）
  base_url: string   // 自定义 API 地址（可选）
  model: string      // 默认模型名称（可选）
}

// =====================================================================
// 后端配置（用于设置页读写）
// =====================================================================

export interface BackendConfig {
  data_providers: DataProvidersConfig
  ai_providers: AIProviderConfig[]
  llm_default: string
}
