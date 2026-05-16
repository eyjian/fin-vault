// 金额工具：统一用 decimal.js 处理，前端永不直接用 number 计算
import Decimal from 'decimal.js'

Decimal.set({ precision: 30 })

export function toDecimal(v: string | number | null | undefined): Decimal {
  if (v === null || v === undefined || v === '') return new Decimal(0)
  try {
    return new Decimal(v)
  } catch {
    return new Decimal(0)
  }
}

export function fmtMoney(v: string | number | null | undefined, currency = 'CNY'): string {
  const d = toDecimal(v)
  const sym = currency === 'CNY' ? '￥' : currency === 'USD' ? '$' : currency === 'HKD' ? 'HK$' : ''
  return sym + d.toFixed(2)
}

export function fmtNumber(v: string | number | null | undefined, dp = 4): string {
  return toDecimal(v).toFixed(dp)
}

export function fmtPercent(v: string | number | null | undefined, dp = 2): string {
  return toDecimal(v).toFixed(dp) + '%'
}

export function pnlColor(v: string | number | null | undefined): string {
  const d = toDecimal(v)
  if (d.isPositive()) return 'fv-pnl-up'
  if (d.isNegative()) return 'fv-pnl-down'
  return ''
}
