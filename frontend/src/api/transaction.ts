import { get, post, del, PageResp } from './http'
import type { Transaction, TxnType } from './types'

export interface TxnListParams {
  page?: number
  page_size?: number
  holding_id?: number
  asset_id?: number
  platform_id?: number
  txn_type?: TxnType
  start?: string
  end?: string
}

export const txnApi = {
  list(params: TxnListParams = {}) {
    return get<PageResp<Transaction>>('/transactions', params)
  },
  get(id: number) {
    return get<Transaction>(`/transactions/${id}`)
  },
  create(t: Transaction) {
    return post<Transaction>('/transactions', t)
  },
  remove(id: number) {
    return del(`/transactions/${id}`)
  }
}
