import { get, post, put, del, PageResp } from './http'
import type { Asset, AssetType } from './types'

export interface AssetListParams {
  page?: number
  page_size?: number
  asset_type?: AssetType
  keyword?: string
  status?: string
  maturing_within_days?: number
}

export const assetApi = {
  list(params: AssetListParams = {}) {
    return get<PageResp<Asset>>('/assets', params)
  },
  funds(params: AssetListParams = {}) {
    return get<PageResp<Asset>>('/assets/funds', params)
  },
  stocks(params: AssetListParams = {}) {
    return get<PageResp<Asset>>('/assets/stocks', params)
  },
  wealth(params: AssetListParams = {}) {
    return get<PageResp<Asset>>('/assets/wealth', params)
  },
  cash(params: AssetListParams = {}) {
    return get<PageResp<Asset>>('/assets/cash', params)
  },
  get(id: number) {
    return get<Asset>(`/assets/${id}`)
  },
  create(a: Asset) {
    return post<Asset>('/assets', a)
  },
  update(id: number, a: Asset) {
    return put<Asset>(`/assets/${id}`, a)
  },
  remove(id: number) {
    return del(`/assets/${id}`)
  }
}
