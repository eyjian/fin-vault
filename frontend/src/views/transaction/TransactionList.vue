<script setup lang="ts">
// 交易流水列表（13 种类型筛选 + 录入 + 删除）

import { ref, reactive, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus, Delete } from '@element-plus/icons-vue'
import { txnApi } from '@/api/transaction'
import { usePlatformStore } from '@/stores/platform'
import type { Transaction, TxnType } from '@/api/types'
import TxnDialog from '@/components/TxnDialog.vue'
import { fmtMoney } from '@/utils/decimal'

const route = useRoute()

const platformStore = usePlatformStore()

const list = ref<Transaction[]>([])
const total = ref(0)
const loading = ref(false)

const filter = reactive({
  txn_type: '' as TxnType | '',
  asset_id: undefined as number | undefined,
  platform_id: undefined as number | undefined,
  start: '',
  end: '',
  page: 1,
  page_size: 20
})

async function fetchList() {
  loading.value = true
  try {
    const r = await txnApi.list({
      page: filter.page,
      page_size: filter.page_size,
      txn_type: filter.txn_type || undefined,
      asset_id: filter.asset_id || undefined,
      platform_id: filter.platform_id || undefined,
      start: filter.start || undefined,
      end: filter.end || undefined
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

async function remove(t: Transaction) {
  if (!t.id) return
  try {
    await ElMessageBox.confirm('删除会同步回滚 Holding，确定？', '确认', { type: 'warning' })
    await txnApi.remove(t.id)
    ElMessage.success('已删除')
    fetchList()
  } catch {
    /* */
  }
}

const txnDialog = ref(false)

const txnTypeOptions: { value: TxnType; label: string }[] = [
  { value: 'buy', label: '买入' },
  { value: 'sell', label: '卖出' },
  { value: 'dividend', label: '现金分红' },
  { value: 'dividend_reinvest', label: '分红再投' },
  { value: 'split', label: '拆股' },
  { value: 'bonus', label: '送股' },
  { value: 'mature', label: '到期' },
  { value: 'interest', label: '利息' },
  { value: 'deposit', label: '充值' },
  { value: 'withdraw', label: '提现' },
  { value: 'cash_in', label: '现金入账' },
  { value: 'cash_out', label: '现金出账' },
  { value: 'adjust', label: '手动调整' }
]

const txnTypeMap = Object.fromEntries(txnTypeOptions.map((o) => [o.value, o.label]))

function initAssetIdFromRoute() {
  const aid = route.query.asset_id
  if (aid) {
    filter.asset_id = Number(aid)
  }
}

onMounted(async () => {
  await platformStore.load()
  initAssetIdFromRoute()
  await fetchList()
})

watch(() => route.query.asset_id, (val) => {
  if (val) {
    filter.asset_id = Number(val)
  } else {
    filter.asset_id = undefined
  }
  filter.page = 1
  fetchList()
})
</script>

<template>
  <div class="fv-page">
    <div class="fv-card">
      <div class="fv-flex" style="margin-bottom: 12px; flex-wrap: wrap;">
        <el-select v-model="filter.txn_type" placeholder="类型" clearable style="width: 130px;" @change="fetchList">
          <el-option v-for="o in txnTypeOptions" :key="o.value" :value="o.value" :label="o.label" />
        </el-select>
        <el-select v-model="filter.platform_id" placeholder="平台" clearable filterable style="width: 160px;" @change="fetchList">
          <el-option v-for="p in platformStore.platforms" :key="p.id" :value="p.id" :label="p.name" />
        </el-select>
        <el-input-number v-model="filter.asset_id" placeholder="资产ID" :min="0" style="width: 120px;" />
        <el-date-picker v-model="filter.start" type="date" placeholder="开始" value-format="YYYY-MM-DD" />
        <el-date-picker v-model="filter.end" type="date" placeholder="结束" value-format="YYYY-MM-DD" />
        <el-button type="primary" :icon="Refresh" @click="fetchList">查询</el-button>
        <div class="fv-grow" />
        <el-button type="primary" :icon="Plus" @click="txnDialog = true">录入流水</el-button>
      </div>

      <el-table :data="list" v-loading="loading" stripe border :max-height="580">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ row.txn_time?.replace('T', ' ').slice(0, 19) }}</template>
        </el-table-column>
        <el-table-column label="类型" width="110">
          <template #default="{ row }">
            <el-tag size="small">{{ txnTypeMap[row.txn_type] || row.txn_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="资产代码" width="120">
          <template #default="{ row }">
            {{ row.asset?.asset_code || row.asset_id }}
          </template>
        </el-table-column>
        <el-table-column label="资产名称" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.asset?.name || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="平台" width="140">
          <template #default="{ row }">{{ platformStore.nameOf(row.platform_id) }}</template>
        </el-table-column>
        <el-table-column prop="quantity" label="数量" width="120" align="right" />
        <el-table-column prop="price" label="单价" width="120" align="right" />
        <el-table-column label="金额" width="140" align="right">
          <template #default="{ row }">{{ fmtMoney(row.amount, row.currency) }}</template>
        </el-table-column>
        <el-table-column label="净额" width="140" align="right">
          <template #default="{ row }">{{ fmtMoney(row.net_amount, row.currency) }}</template>
        </el-table-column>
        <el-table-column prop="currency" label="币种" width="70" />
        <el-table-column prop="source" label="来源" width="100" />
        <el-table-column prop="note" label="备注" min-width="160" show-overflow-tooltip />
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div style="margin-top: 12px; text-align: right;">
        <el-pagination
          v-model:current-page="filter.page"
          v-model:page-size="filter.page_size"
          :total="total"
          :page-sizes="[20, 50, 100, 200]"
          layout="total, sizes, prev, pager, next"
          @current-change="fetchList"
          @size-change="fetchList"
        />
      </div>
    </div>

    <TxnDialog
      v-model="txnDialog"
      :allowed-types="['buy', 'sell', 'dividend', 'dividend_reinvest', 'split', 'bonus', 'mature', 'interest', 'deposit', 'withdraw', 'cash_in', 'cash_out', 'adjust']"
      default-txn-type="buy"
      @submitted="fetchList"
    />
  </div>
</template>
