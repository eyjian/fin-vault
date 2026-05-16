import { get, patch, PageResp } from './http'
import type { HoldingView, HoldingSummary, AssetType } from './types'

export interface HoldingListParams {
  page?: number
  page_size?: number
  asset_type?: AssetType
  platform_id?: number
  status?: string
  display_currency?: 'raw' | 'CNY'
}

export const holdingApi = {
  list(params: HoldingListParams = {}) {
    return get<PageResp<HoldingView>>('/holdings', params)
  },
  summary(displayCurrency: 'raw' | 'CNY' = 'CNY') {
    return get<HoldingSummary>('/holdings/summary', { display_currency: displayCurrency })
  },
  get(id: number) {
    return get<HoldingView>(`/holdings/${id}`)
  },
  setCostMethod(id: number, costMethod: 'weighted_avg' | 'fifo') {
    return patch(`/holdings/${id}`, { cost_method: costMethod })
  }
}
