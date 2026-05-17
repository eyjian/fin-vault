<script setup lang="ts">
// 股票管理页（用户必交付项）
//   - 录入：股票主表 + StockDetail（market 必填，HK/US/SH/SZ/BJ）
//   - 列表：代码 / 名称 / 市场 / 行业 / 板块 / 最新价 / 涨跌幅 / 平台 / 币种 / 操作
//   - 流水：覆盖 5 种类型 buy/sell/dividend/split/bonus

import { assetApi } from '@/api/asset'
import { quoteApi } from '@/api/quote'
import type { Asset, StockDetail } from '@/api/types'
import MoneyInput from '@/components/MoneyInput.vue'
import TxnDialog from '@/components/TxnDialog.vue'
import { usePlatformStore } from '@/stores/platform'
import { Delete, Edit, Money, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'

const platformStore = usePlatformStore()

const list = ref<Asset[]>([])
const total = ref(0)
const loading = ref(false)
const filter = reactive({ keyword: '', market: '', status: '', page: 1, page_size: 20 })

async function fetchList() {
  loading.value = true
  try {
    const r = await assetApi.stocks({
      page: filter.page,
      page_size: filter.page_size,
      keyword: filter.keyword || undefined,
      status: filter.status || undefined
    })
    list.value = (r?.items || r?.list || []).filter((a: Asset) =>
      filter.market ? a.stock_detail?.market === filter.market : true
    )
    total.value = r?.total || 0
  } catch {
    list.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

const formVisible = ref(false)
const isEdit = ref(false)
const form = ref<Asset>(emptyForm())

function emptyForm(): Asset {
  return {
    asset_code: '',
    name: '',
    asset_type: 'stock',
    currency: 'CNY',
    status: 'active',
    issuer_platform_id: undefined,
    risk_level: '',
    remark: '',
    stock_detail: {
      market: 'SH',
      industry: '',
      sector: '',
      latest_price: ''
    } as StockDetail
  }
}

function openCreate() {
  form.value = emptyForm()
  isEdit.value = false
  formVisible.value = true
}

function openEdit(a: Asset) {
  form.value = JSON.parse(JSON.stringify(a))
  if (!form.value.stock_detail) form.value.stock_detail = { market: 'SH' } as StockDetail
  isEdit.value = true
  formVisible.value = true
}

function sanitizePayload(a: Asset): Asset {
  const p: any = JSON.parse(JSON.stringify(a))
  if (p.stock_detail) {
    const sd = p.stock_detail
    if (sd.latest_price === '' || sd.latest_price == null) delete sd.latest_price
    if (sd.total_shares === '' || sd.total_shares == null) delete sd.total_shares
    if (!sd.listing_date) delete sd.listing_date
    if (!sd.latest_price_time) delete sd.latest_price_time
  }
  if (p.issuer_platform_id == null) delete p.issuer_platform_id
  if (!p.risk_level) delete p.risk_level
  return p as Asset
}

async function submitForm() {
  if (!form.value.asset_code || !form.value.name) {
    ElMessage.warning('代码与名称必填')
    return
  }
  if (!form.value.stock_detail?.market) {
    ElMessage.warning('请选择市场')
    return
  }
  try {
    const payload = sanitizePayload(form.value)
    if (isEdit.value && form.value.id) {
      await assetApi.update(form.value.id, payload)
      ElMessage.success('更新成功')
    } else {
      await assetApi.create(payload)
      ElMessage.success('新增成功')
    }
    formVisible.value = false
    fetchList()
  } catch {
    /* 全局拦截器已弹错误 */
  }
}

async function remove(a: Asset) {
  if (!a.id) return
  try {
    await ElMessageBox.confirm(`确定删除股票 ${a.name}（${a.asset_code}）？`, '确认', { type: 'warning' })
    await assetApi.remove(a.id)
    ElMessage.success('已删除')
    fetchList()
  } catch {
    /* 取消 */
  }
}

const refreshing = ref(false)
async function refreshAll() {
  if (list.value.length === 0) return
  refreshing.value = true
  try {
    const ids = list.value.map((a) => a.id!).filter(Boolean) as number[]
    const res = await quoteApi.refresh({ asset_ids: ids, source: 'auto' })
    const ok = (res || []).filter((r) => r.ok).length
    ElMessage.success(`刷新完成：成功 ${ok} / ${res?.length || 0}`)
    fetchList()
  } catch {
    /* */
  } finally {
    refreshing.value = false
  }
}

const txnDialog = ref(false)
const txnAsset = ref<Asset | null>(null)
function openTxn(a: Asset) {
  txnAsset.value = a
  txnDialog.value = true
}

const marketLabel = (m: string) =>
  ({ SH: '沪 A', SZ: '深 A', HK: '港股', US: '美股', BJ: '北交所' } as Record<string, string>)[m] || m

onMounted(async () => {
  await platformStore.load()
  await fetchList()
})

const platformName = computed(() => (id?: number | null) => platformStore.nameOf(id))
</script>

<template>
  <div class="fv-page">
    <div class="fv-card" style="margin-bottom: 16px;">
      <div class="fv-flex" style="margin-bottom: 12px;">
        <el-input v-model="filter.keyword" placeholder="搜索代码/名称" style="width: 240px;" clearable
          @clear="fetchList" @keyup.enter="fetchList" />
        <el-select v-model="filter.market" placeholder="市场" clearable style="width: 120px;" @change="fetchList">
          <el-option label="沪 A" value="SH" />
          <el-option label="深 A" value="SZ" />
          <el-option label="港股" value="HK" />
          <el-option label="美股" value="US" />
          <el-option label="北交所" value="BJ" />
        </el-select>
        <el-select v-model="filter.status" placeholder="状态" clearable style="width: 120px;" @change="fetchList">
          <el-option label="active" value="active" />
          <el-option label="delisted" value="delisted" />
        </el-select>
        <el-button type="primary" @click="fetchList">查询</el-button>
        <div class="fv-grow" />
        <el-button :icon="Refresh" :loading="refreshing" @click="refreshAll">刷新行情</el-button>
        <el-button type="primary" :icon="Plus" @click="openCreate">新增股票</el-button>
      </div>

      <el-table :data="list" v-loading="loading" stripe border :max-height="540">
        <el-table-column prop="asset_code" label="股票代码" width="100" />
        <el-table-column prop="name" label="名称" min-width="160" show-overflow-tooltip />
        <el-table-column label="市场" width="80">
          <template #default="{ row }">{{ marketLabel(row.stock_detail?.market) }}</template>
        </el-table-column>
        <el-table-column prop="stock_detail.industry" label="行业" width="120" show-overflow-tooltip />
        <el-table-column prop="stock_detail.sector" label="板块" width="100" show-overflow-tooltip />
        <el-table-column label="最新价" width="100" align="right">
          <template #default="{ row }">{{ row.stock_detail?.latest_price || '-' }}</template>
        </el-table-column>
        <el-table-column label="平台" width="140">
          <template #default="{ row }">{{ platformName(row.issuer_platform_id) || '-' }}</template>
        </el-table-column>
        <el-table-column prop="currency" label="币种" width="70" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Money" @click="openTxn(row)">录流水</el-button>
            <el-button size="small" :icon="Edit" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div style="margin-top: 12px; text-align: right;">
        <el-pagination
          v-model:current-page="filter.page"
          v-model:page-size="filter.page_size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="fetchList"
          @size-change="fetchList"
        />
      </div>
    </div>

    <!-- 录入 / 编辑 -->
    <el-dialog v-model="formVisible" :title="isEdit ? '编辑股票' : '新增股票'" width="720px" destroy-on-close>
      <el-form :model="form" label-width="100px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="股票代码" required>
              <el-input v-model="form.asset_code" placeholder="例如 600519" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="市场" required>
              <el-select v-model="form.stock_detail!.market">
                <el-option label="沪 A (SH)" value="SH" />
                <el-option label="深 A (SZ)" value="SZ" />
                <el-option label="港股 (HK)" value="HK" />
                <el-option label="美股 (US)" value="US" />
                <el-option label="北交所 (BJ)" value="BJ" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="例如 贵州茅台" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="行业">
              <el-input v-model="form.stock_detail!.industry" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="板块">
              <el-input v-model="form.stock_detail!.sector" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="上市日">
              <el-date-picker v-model="form.stock_detail!.listing_date" type="date" value-format="YYYY-MM-DD" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="最新价">
              <MoneyInput v-model="form.stock_detail!.latest_price as string" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="持仓平台">
              <el-select v-model="form.issuer_platform_id" placeholder="选择平台" clearable filterable>
                <el-option v-for="p in platformStore.platforms" :key="p.id" :value="p.id" :label="p.name" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="币种">
              <el-select v-model="form.currency">
                <el-option label="CNY" value="CNY" />
                <el-option label="HKD" value="HKD" />
                <el-option label="USD" value="USD" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm">{{ isEdit ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <TxnDialog
      v-model="txnDialog"
      :asset-id="txnAsset?.id"
      :asset-code="txnAsset?.asset_code"
      :asset-name="txnAsset?.name"
      asset-type="stock"
      :currency="txnAsset?.currency || 'CNY'"
      :allowed-types="['buy', 'sell', 'dividend', 'split', 'bonus']"
      default-txn-type="buy"
      @submitted="fetchList"
    />
  </div>
</template>
