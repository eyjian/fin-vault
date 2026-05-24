<script setup lang="ts">
// PulseDiagnosisCell —— 资产管理页表格中的"AI 把脉"单元格组件。
//
// 设计要点（与 design.md D6 紧凑方案 + spec ai-pulse-diagnosis 对齐）：
//   - tag 颜色映射：sell→danger / reduce→warning / hold→success / add→primary
//   - 置信度为 low 时在 tag 旁加灰色感叹号 + tooltip "请谨慎参考"
//   - tag 可点击，弹 popover 分层展示：summary 简要 + "查看详细分析"按钮展开 detail
//   - "AI 把脉"按钮触发新把脉（消耗 token），由父组件传入 onDiagnose 处理
//   - 距今天数 ≥ 7 天时显示"上次把脉于 X 天前"提示

import { MagicStick, QuestionFilled, WarningFilled } from '@element-plus/icons-vue';
import { computed, ref } from 'vue';

import type { PulseConfidence, PulseDiagnosisResult, PulseRecommendation } from '@/api/types';

const props = defineProps<{
  assetId: number | undefined
  result: PulseDiagnosisResult | undefined
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'diagnose', assetId: number): void
}>()

// tag 颜色映射
const tagType = computed<'danger' | 'warning' | 'success' | 'primary' | 'info'>(() => {
  const r = props.result?.recommendation as PulseRecommendation | undefined
  switch (r) {
    case 'sell':
      return 'danger'
    case 'reduce':
      return 'warning'
    case 'hold':
      return 'success'
    case 'add':
      return 'primary'
    default:
      return 'info'
  }
})

// tag 文案
const tagText = computed(() => {
  const r = props.result?.recommendation as PulseRecommendation | undefined
  switch (r) {
    case 'sell':
      return '建议卖出'
    case 'reduce':
      return '建议减仓'
    case 'hold':
      return '继续持有'
    case 'add':
      return '建议加仓'
    default:
      return '-'
  }
})

// 置信度文案
const confidenceText = computed(() => {
  const c = props.result?.confidence as PulseConfidence | undefined
  switch (c) {
    case 'high':
      return '高'
    case 'medium':
      return '中'
    case 'low':
      return '低'
    default:
      return ''
  }
})

const isLowConfidence = computed(() => props.result?.confidence === 'low')

// 是否有把脉结果
const hasResult = computed(() => {
  return !!props.result && props.result.status === 'success' && !!props.result.recommendation
})

// 失败状态
const isFailed = computed(() => props.result?.status === 'failed')

// 距今天数
const daysAgo = computed(() => {
  const at = props.result?.diagnosed_at
  if (!at) return -1
  const t = new Date(at).getTime()
  if (isNaN(t)) return -1
  const diff = Date.now() - t
  return Math.floor(diff / (1000 * 60 * 60 * 24))
})

// 距今 ≥ 7 天提示
const showStaleHint = computed(() => daysAgo.value >= 7)

// popover 控制：是否展示详细分析。
// 说明：el-popover 的 trigger="click" 会自动监听 reference 点击并切换显/隐，
// 不能再在 reference 上加 @click 去 manual 切换，否则会两次切换手衢集体（别出问题是弹出后马上关闭）。
const showDetail = ref(false)

// popover 打开前：重置详细分析为折叠状态
function onPopoverShow() {
  showDetail.value = false
}

function onDiagnoseClick() {
  if (!props.assetId) return
  emit('diagnose', props.assetId)
}
</script>

<template>
  <div class="pulse-cell">
    <!-- 把脉按钮（始终展示） -->
    <el-tooltip content="AI 把脉（消耗 token，请谨慎使用）" placement="top">
      <el-button
        size="small"
        :icon="MagicStick"
        :loading="loading"
        :disabled="!assetId"
        @click="onDiagnoseClick"
        circle
      />
    </el-tooltip>

    <!-- 把脉结果（只在有 success 结果时展示） -->
    <template v-if="hasResult">
      <el-popover
        placement="bottom"
        :width="380"
        trigger="click"
        @before-enter="onPopoverShow"
      >
        <template #reference>
          <el-tag
            :type="tagType"
            size="small"
            class="pulse-tag"
          >
            {{ tagText }}
          </el-tag>
        </template>
        <div class="pulse-popover">
          <div class="pulse-popover-header">
            <strong>{{ tagText }}</strong>
            <span v-if="confidenceText" class="pulse-confidence" :class="{ 'is-low': isLowConfidence }">
              置信度：{{ confidenceText }}
              <el-tooltip v-if="isLowConfidence" content="数据不足或信号矛盾，请谨慎参考" placement="top">
                <el-icon class="warn-icon"><WarningFilled /></el-icon>
              </el-tooltip>
            </span>
          </div>
          <div class="pulse-popover-summary">
            {{ result?.summary || '-' }}
          </div>
          <div v-if="!showDetail" class="pulse-popover-actions">
            <el-button size="small" type="primary" link @click="showDetail = true">查看详细分析 →</el-button>
          </div>
          <div v-else class="pulse-popover-detail">
            <div class="detail-title">详细分析</div>
            <div class="detail-content">{{ result?.detail || '-' }}</div>
          </div>
          <div v-if="result?.diagnosed_at" class="pulse-popover-footer">
            把脉于 {{ result.diagnosed_at.replace('T', ' ').slice(0, 19) }}
            <span v-if="result.trigger_source === 'chat'">（AI 对话触发）</span>
            <span v-else-if="result.trigger_source === 'scheduled'">（定时任务）</span>
          </div>
        </div>
      </el-popover>

      <!-- 低置信度警告图标（tag 旁） -->
      <el-tooltip v-if="isLowConfidence" content="请谨慎参考" placement="top">
        <el-icon class="confidence-warn"><WarningFilled /></el-icon>
      </el-tooltip>

      <!-- 距今 ≥ 7 天提醒 -->
      <el-tooltip v-if="showStaleHint" :content="`上次把脉于 ${daysAgo} 天前，建议重新把脉`" placement="top">
        <el-icon class="stale-icon"><QuestionFilled /></el-icon>
      </el-tooltip>
    </template>

    <!-- 失败状态 -->
    <el-tooltip v-else-if="isFailed" :content="result?.error_message || '把脉失败'" placement="top">
      <el-tag type="danger" size="small">失败</el-tag>
    </el-tooltip>
  </div>
</template>

<style scoped>
.pulse-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.pulse-tag {
  cursor: pointer;
  user-select: none;
}

.pulse-popover {
  font-size: 13px;
  line-height: 1.6;
}

.pulse-popover-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.pulse-confidence {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.pulse-confidence.is-low {
  color: var(--el-color-warning);
}

.pulse-popover-summary {
  color: var(--el-text-color-primary);
  margin-bottom: 8px;
}

.pulse-popover-actions {
  text-align: right;
}

.pulse-popover-detail {
  margin-top: 4px;
}

.detail-title {
  font-weight: 600;
  margin-bottom: 4px;
  color: var(--el-text-color-primary);
}

.detail-content {
  white-space: pre-wrap;
  color: var(--el-text-color-regular);
  background: var(--el-fill-color-light);
  padding: 8px;
  border-radius: 4px;
  max-height: 280px;
  overflow-y: auto;
}

.pulse-popover-footer {
  margin-top: 8px;
  padding-top: 6px;
  border-top: 1px solid var(--el-border-color-lighter);
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.confidence-warn {
  color: var(--el-color-warning);
  font-size: 14px;
}

.stale-icon {
  color: var(--el-text-color-secondary);
  font-size: 14px;
  cursor: help;
}

.warn-icon {
  color: var(--el-color-warning);
  font-size: 13px;
}
</style>
