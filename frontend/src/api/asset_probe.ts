// 资产录入"按代码自动填充"前端 API 封装（asset-form-autofill）。
//
// 后端契约：GET /api/v1/assets/probe?asset_type=fund|stock&asset_code=xxx&market=SH
//   - 200 → AssetProbeResult（仅含远端能取到的字段，其它 omitempty）
//   - 400 → 参数非法
//   - 404 → ErrAssetProbeNotFound（未找到该代码对应的公开信息）
//   - 502 → ErrAssetProbeUpstream（远端 API 错误）
//   - 401 → 未登录（由全局拦截）

import { get } from './http'
import type { AssetProbeParams, AssetProbeResult } from './types'

export const assetProbeApi = {
  probe(params: AssetProbeParams): Promise<AssetProbeResult> {
    // get<T> 已在 http.ts 中处理拆包；失败会抛错，调用方在 try/catch 中处理。
    return get<AssetProbeResult>('/assets/probe', params as unknown as Record<string, unknown>)
  }
}
