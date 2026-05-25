<script setup lang="ts">
// 持仓视图：含计算字段 market_value / pnl
import { ref, reactive, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { holdingApi } from '@/api/holding'
import { usePlatformStore } from '@/stores/platform'
import type { HoldingView, AssetType } from '@/api/types'
import { fmtMoney, pnlColor, toDecimal } from '@/utils/decimal'

const platformStore = usePlatformStore()
const list = ref<HoldingView[]>([])
const total = ref(0)
const loading = ref(false)
const filter = reactive({
  asset_type: '' as AssetType | '',
  platform_id: undefined as number | undefined,
  status: '' as string,
  display_currency: 'CNY' as 'CNY' | 'raw',
  page: 1,
  page_size: 30
})

async function fetchList() {
  loading.value = true
  try {
    const r = await holdingApi.list({
      page: filter.page,
      page_size: filter.page_size,
      asset_type: filter.asset_type || undefined,
      platform_id: filter.platform_id,
      status: filter.status || undefined,
      display_currency: filter.display_currency
    })
    list.value = r?.items || r?.list || []
    total.value = r?.total || 0
  } catch {
    list.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await platformStore.load()
  await fetchList()
})
</script>

<template>
  <div class="fv-page">
    <div class="fv-card">
      <div class="fv-flex" style="margin-bottom: 12px;">
        <el-select v-model="filter.asset_type" placeholder="类型" clearable style="width: 130px;" @change="fetchList">
          <el-option label="基金" value="fund" />
          <el-option label="股票" value="stock" />
          <el-option label="理财" value="wealth" />
          <el-option label="现金" value="cash" />
        </el-select>
        <el-select v-model="filter.platform_id" placeholder="平台" clearable filterable style="width: 160px;" @change="fetchList">
          <el-option v-for="p in platformStore.platforms" :key="p.id" :value="p.id" :label="p.name" />
        </el-select>
        <el-select v-model="filter.status" placeholder="状态" clearable style="width: 120px;" @change="fetchList">
          <el-option label="持仓中" value="持有中" />
          <el-option label="已清仓" value="已关闭" />
          <el-option label="到期" value="已到期" />
        </el-select>
        <el-radio-group v-model="filter.display_currency" @change="fetchList">
          <el-radio-button value="CNY">CNY</el-radio-button>
          <el-radio-button value="raw">原币种</el-radio-button>
        </el-radio-group>
        <div class="fv-grow" />
        <el-button :icon="Refresh" @click="fetchList">刷新</el-button>
      </div>

      <el-table :data="list" v-loading="loading" stripe border :max-height="600">
        <el-table-column label="代码" width="110">
          <template #default="{ row }">{{ row.asset?.asset_code }}</template>
        </el-table-column>
        <el-table-column label="名称" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ row.asset?.name }}</template>
        </el-table-column>
        <el-table-column label="类型" width="80">
          <template #default="{ row }">{{ row.asset?.asset_type }}</template>
        </el-table-column>
        <el-table-column label="平台" width="140">
          <template #default="{ row }">{{ platformStore.nameOf(row.platform_id) }}</template>
        </el-table-column>
        <el-table-column label="数量" width="120" align="right" prop="quantity" />
        <el-table-column label="均价" width="120" align="right" prop="avg_cost" />
        <el-table-column label="累计成本" width="130" align="right">
          <template #default="{ row }">{{ fmtMoney(row.total_cost, row.asset?.currency) }}</template>
        </el-table-column>
        <el-table-column label="最新价" width="110" align="right" prop="latest_price" />
        <el-table-column label="市值" width="130" align="right">
          <template #default="{ row }">{{ fmtMoney(row.market_value, row.asset?.currency) }}</template>
        </el-table-column>
        <el-table-column label="浮动盈亏" width="130" align="right">
          <template #default="{ row }">
            <span :class="pnlColor(row.unrealized_pnl)">{{ fmtMoney(row.unrealized_pnl, row.asset?.currency) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="收益率" width="90" align="right">
          <template #default="{ row }">
            <span :class="pnlColor(row.pnl_ratio)">
              {{ row.pnl_ratio ? toDecimal(row.pnl_ratio).mul(100).toFixed(2) + '%' : '-' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag size="small">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
      </el-table>

      <div style="margin-top: 12px; text-align: right;">
        <el-pagination
          v-model:current-page="filter.page"
          v-model:page-size="filter.page_size"
          :total="total"
          layout="total, prev, pager, next"
          @current-change="fetchList"
        />
      </div>
    </div>
  </div>
</template>
