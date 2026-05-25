// 后端配置 API（用于设置页读写多 API 服务商配置等）。
//
// GET /api/v1/config/data_providers → { data_providers: DataProvidersConfig }
// PUT /api/v1/config/data_providers → { data_providers: DataProvidersConfig }

import { get, put } from './http'
import type { DataProvidersConfig } from './types'

// 后端返回的是 { data_providers: DataProvidersConfig }，get/put 自动拆了外层 data，
// 所以这里拿到的是 { data_providers: ... }
interface DataProvidersResp {
  data_providers: DataProvidersConfig
}

export const configApi = {
  async getDataProviders(): Promise<DataProvidersConfig> {
    const resp = await get<DataProvidersResp>('/config/data_providers')
    return resp.data_providers
  },
  async updateDataProviders(cfg: DataProvidersConfig): Promise<DataProvidersConfig> {
    const resp = await put<DataProvidersResp>('/config/data_providers', { data_providers: cfg })
    return resp.data_providers
  }
}
