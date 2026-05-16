import { get, post } from './http'
import type { PriceQuote } from './types'

export interface RefreshReq {
  asset_ids?: number[]
  source?: 'auto' | 'eastmoney' | 'sina' | 'tencent'
}

export interface RefreshResult {
  asset_id: number
  source?: string
  price?: string
  ok: boolean
  message?: string
}

export interface LatestQuote {
  asset_id: number
  price: string
  change_pct?: string
  volume?: string
  quote_time?: string
  source?: string
}

export const quoteApi = {
  latest(assetIDs: number[]) {
    if (assetIDs.length === 0) return Promise.resolve([] as LatestQuote[])
    return get<LatestQuote[]>('/quotes/latest', { asset_ids: assetIDs.join(',') })
  },
  refresh(req: RefreshReq) {
    return post<RefreshResult[]>('/quotes/refresh', req)
  },
  saveManual(q: PriceQuote) {
    return post<PriceQuote>('/quotes', q)
  }
}
