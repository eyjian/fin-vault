// frontend/src/api/pulse-diagnosis.ts
//
// AI 把脉 REST API 调用层（spec ai-pulse-diagnosis）。
//
// 设计要点：
//   - create(assetIds)：POST 触发新把脉（消耗 token），后端并行执行
//   - batchGet(assetIds)：GET 仅查缓存（不触发新把脉），用于资产管理页预加载
//   - 单个资产失败不阻塞其他资产，前端按 item.status 判别成功/失败
//   - assetIds 为空数组时 batchGet 返回当前用户全部把脉结果
import { get, post } from './http'
import type { PulseDiagnosisResp } from './types'

export const pulseDiagnosisApi = {
  // 触发批量把脉（消耗 token，后端用 errgroup + 信号量并行执行）。
  // 单资产把脉传 [id] 即可；并发度由后端配置 ai.pulse_diagnosis.concurrency 控制。
  create(assetIds: number[]) {
    return post<PulseDiagnosisResp>('/ai/pulse-diagnosis', { asset_ids: assetIds })
  },

  // 批量取已有把脉结果（仅查数据库，不触发新把脉）。
  // assetIds 非空：仅返回这些资产的最近一次把脉；空数组/undefined：返回当前用户全部。
  batchGet(assetIds?: number[]) {
    if (!assetIds || assetIds.length === 0) {
      return get<PulseDiagnosisResp>('/ai/pulse-diagnosis')
    }
    return get<PulseDiagnosisResp>('/ai/pulse-diagnosis', {
      asset_ids: assetIds.join(',')
    })
  }
}
