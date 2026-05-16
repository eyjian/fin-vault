<script setup lang="ts">
// 总览：按类型/平台/币种聚合 + 总盈亏卡片

import { ref, onMounted, computed } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { holdingApi } from '@/api/holding'
import type { HoldingSummary } from '@/api/types'
import { fmtMoney, fmtPercent, pnlColor, toDecimal } from '@/utils/decimal'

const summary = ref<HoldingSummary | null>(null)
const displayCurrency = ref<'CNY' | 'raw'>('CNY')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    summary.value = await holdingApi.summary(displayCurrency.value)
  } catch {
    summary.value = null
  } finally {
    loading.value = false
  }
}

const pnlClass = computed(() => pnlColor(summary.value?.total_pnl))

onMounted(load)
</script>

<template>
  <div class="fv-page">
    <div class="fv-flex" style="margin-bottom: 16px;">
      <el-radio-group v-model="displayCurrency" @change="load">
        <el-radio-button value="CNY">CNY 折算</el-radio-button>
        <el-radio-button value="raw">原币种</el-radio-button>
      </el-radio-group>
      <div class="fv-grow" />
      <el-button :icon="Refresh" @click="load" :loading="loading">刷新</el-button>
    </div>

    <el-row :gutter="16">
      <el-col :span="6">
        <div class="fv-card">
          <div style="color: var(--fv-text-muted); font-size: 12px;">总市值</div>
          <div style="font-size: 28px; font-weight: 600; margin-top: 4px;">
            {{ fmtMoney(summary?.total_market_value, displayCurrency === 'CNY' ? 'CNY' : '') }}
          </div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="fv-card">
          <div style="color: var(--fv-text-muted); font-size: 12px;">总成本</div>
          <div style="font-size: 28px; font-weight: 600; margin-top: 4px;">
            {{ fmtMoney(summary?.total_cost, displayCurrency === 'CNY' ? 'CNY' : '') }}
          </div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="fv-card">
          <div style="color: var(--fv-text-muted); font-size: 12px;">总盈亏</div>
          <div style="font-size: 28px; font-weight: 600; margin-top: 4px;" :class="pnlClass">
            {{ fmtMoney(summary?.total_pnl, displayCurrency === 'CNY' ? 'CNY' : '') }}
          </div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="fv-card">
          <div style="color: var(--fv-text-muted); font-size: 12px;">总收益率</div>
          <div style="font-size: 28px; font-weight: 600; margin-top: 4px;" :class="pnlClass">
            {{ summary?.pnl_ratio ? (toDecimal(summary.pnl_ratio).mul(100).toFixed(2) + '%') : '-' }}
          </div>
        </div>
      </el-col>
    </el-row>

    <el-row :gutter="16" style="margin-top: 16px;">
      <el-col :span="8">
        <div class="fv-card">
          <div style="font-weight: 600; margin-bottom: 12px;">按资产类型</div>
          <el-table :data="summary?.by_type || []" size="small" border stripe>
            <el-table-column prop="asset_type" label="类型" width="90" />
            <el-table-column label="市值" align="right">
              <template #default="{ row }">{{ fmtMoney(row.market_value) }}</template>
            </el-table-column>
            <el-table-column label="占比" width="80" align="right">
              <template #default="{ row }">{{ toDecimal(row.ratio).mul(100).toFixed(1) }}%</template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="fv-card">
          <div style="font-weight: 600; margin-bottom: 12px;">按平台</div>
          <el-table :data="summary?.by_platform || []" size="small" border stripe>
            <el-table-column prop="platform_name" label="平台" />
            <el-table-column label="市值" align="right">
              <template #default="{ row }">{{ fmtMoney(row.market_value) }}</template>
            </el-table-column>
            <el-table-column label="占比" width="80" align="right">
              <template #default="{ row }">{{ toDecimal(row.ratio).mul(100).toFixed(1) }}%</template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="fv-card">
          <div style="font-weight: 600; margin-bottom: 12px;">按币种</div>
          <el-table :data="summary?.by_currency || []" size="small" border stripe>
            <el-table-column prop="currency" label="币种" width="90" />
            <el-table-column label="市值" align="right">
              <template #default="{ row }">{{ fmtMoney(row.market_value, row.currency) }}</template>
            </el-table-column>
            <el-table-column label="占比" width="80" align="right">
              <template #default="{ row }">{{ toDecimal(row.ratio).mul(100).toFixed(1) }}%</template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
    </el-row>
  </div>
</template>
