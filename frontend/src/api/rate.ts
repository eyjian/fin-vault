import { get, post } from './http'
import type { ExchangeRate } from './types'

export const rateApi = {
  latest(from: string, to: string, asOf?: string) {
    return get<ExchangeRate>('/rates', { from, to, as_of: asOf })
  },
  list(from: string, to: string, start?: string, end?: string) {
    return get<ExchangeRate[]>('/rates/list', { from, to, start, end })
  },
  save(r: ExchangeRate) {
    return post<ExchangeRate>('/rates', r)
  }
}
