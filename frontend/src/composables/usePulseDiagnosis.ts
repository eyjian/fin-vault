// frontend/src/composables/usePulseDiagnosis.ts
//
// AI 把脉前端状态管理（资产管理页共享逻辑，spec ai-pulse-diagnosis）。
//
// 设计要点（与 design.md D6/D7/D9 + spec ai-pulse-diagnosis 对齐）：
//   - 一个 useXxx 实例对应一个资产页面（fund/stock/wealth），3 页各自独立状态
//   - cacheMap：assetId → 把脉结果，用于表格列实时渲染（响应式）
//   - loadingMap：assetId → 是否正在把脉，用于按钮 loading 态
//   - progress：批量把脉进度（"X/Y 完成"）
//   - preload(assetIds)：页面加载时调用 GET 接口预加载已有结果（不消耗 token）
//   - diagnose(assetIds)：触发新把脉（调 POST 接口）；后端并行执行
//
// 注意：
//   - assetId 类型为 number（与 Asset.id 对齐）
//   - batchRunning 用 reactive 对象包裹（state.batchRunning），保证在模板中
//     `pulse.state.batchRunning` 能正确响应；若用裸 ref，调用方拿到的是嵌套
//     ref，模板不会自动解包，会导致按钮 loading 一直为 truthy。
import { ElMessage } from 'element-plus'
import { reactive } from 'vue'

import { pulseDiagnosisApi } from '@/api/pulse-diagnosis'
import type { PulseDiagnosisResult } from '@/api/types'

export function usePulseDiagnosis() {
  // 资产 ID → 最新把脉结果
  const cacheMap = reactive<Record<number, PulseDiagnosisResult>>({})
  // 资产 ID → 是否正在把脉中
  const loadingMap = reactive<Record<number, boolean>>({})
  // 批量把脉进度 + 是否处于"批量把脉中"（统一放在 reactive 对象中，避免裸 ref 在模板中不解包问题）
  const state = reactive({
    done: 0,
    total: 0,
    batchRunning: false
  })

  // 取单个资产的缓存结果（响应式）
  function getResult(assetId: number | undefined | null): PulseDiagnosisResult | undefined {
    if (!assetId) return undefined
    return cacheMap[assetId]
  }

  // 取单个资产是否正在把脉
  function isLoading(assetId: number | undefined | null): boolean {
    if (!assetId) return false
    return !!loadingMap[assetId]
  }

  // 预加载（GET，仅查缓存，不消耗 token）。
  // assetIds 非空时按 IN 过滤；空数组时返回全部。
  async function preload(assetIds: number[]) {
    try {
      const r = await pulseDiagnosisApi.batchGet(assetIds.length > 0 ? assetIds : undefined)
      for (const item of r?.items || []) {
        cacheMap[item.asset_id] = item
      }
    } catch {
      // 静默失败：预加载失败不应阻塞页面，错误已被全局拦截器弹出
    }
  }

  // 触发把脉（POST，消耗 token）。
  // 后端用 errgroup + 信号量并行执行；前端在批量场景下按返回顺序刷新 cacheMap。
  // 单资产模式：assetIds 长度为 1，返回单条结果；批量场景同样统一处理。
  async function diagnose(assetIds: number[]): Promise<PulseDiagnosisResult[]> {
    if (assetIds.length === 0) return []
    // 标记 loading
    for (const id of assetIds) loadingMap[id] = true
    state.done = 0
    state.total = assetIds.length
    state.batchRunning = true

    try {
      const r = await pulseDiagnosisApi.create(assetIds)
      const items = r?.items || []
      // 按 asset_id 写回缓存
      let successCount = 0
      let failCount = 0
      for (const item of items) {
        cacheMap[item.asset_id] = item
        if (item.status === 'success') successCount++
        else failCount++
      }
      state.done = items.length
      // 提示汇总
      if (assetIds.length > 1) {
        if (failCount === 0) {
          ElMessage.success(`AI 把脉完成：成功 ${successCount} 项`)
        } else {
          ElMessage.warning(`AI 把脉完成：成功 ${successCount}，失败 ${failCount}`)
        }
      } else if (items.length > 0) {
        const it = items[0]
        if (it.status === 'success') {
          ElMessage.success('AI 把脉完成')
        } else {
          ElMessage.warning('AI 把脉失败：' + (it.error_message || '未知错误'))
        }
      }
      return items
    } catch {
      // 全局错误拦截器已弹出错误；这里不再重复提示
      return []
    } finally {
      for (const id of assetIds) loadingMap[id] = false
      state.batchRunning = false
    }
  }

  return {
    cacheMap,
    loadingMap,
    state,
    getResult,
    isLoading,
    preload,
    diagnose
  }
}
