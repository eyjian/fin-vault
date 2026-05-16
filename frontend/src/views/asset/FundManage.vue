<script setup lang="ts">
// 基金管理页（用户必交付项）
//   - 列表：代码 / 名称 / 类型 / 公司 / 经理 / 净值 / 净值日 / 平台 / 币种 / 状态 / 操作
//   - 筛选：关键词 / 状态
//   - 录入：基金主表 + FundDetail
//   - 操作：编辑 / 刷新净值（quotes/refresh）/ 录入流水（buy/sell/dividend/dividend_reinvest）

import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Plus, Edit, Delete, Money } from '@element-plus/icons-vue'
import { assetApi } from '@/api/asset'
import { quoteApi } from '@/api/quote'
import { usePlatformStore } from '@/stores/platform'
import type { Asset, FundDetail } from '@/api/types'
import MoneyInput from '@/components/MoneyInput.vue'
import TxnDialog from '@/components/TxnDialog.vue'

const platformStore = usePlatformStore()

const list = ref<Asset[]>([])
const total = ref(0)
const loading = ref(false)
const filter = reactive({ keyword: '', status: '' as string, page: 1, page_size: 20 })

async function fetchList() {
  loading.value = true
  try {
    const r = await assetApi.funds({
      page: filter.page,
      page_size: filter.page_size,
      keyword: filter.keyword || undefined,
      status: filter.status || undefined
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

// === 录入 / 编辑表单 ===
const formVisible = ref(false)
const isEdit = ref(false)
const form = ref<Asset>(emptyForm())

function emptyForm(): Asset {
  return {
    asset_code: '',
    name: '',
    asset_type: 'fund',
    currency: 'CNY',
    status: 'active',
    issuer_platform_id: undefined,
    risk_level: '',
    remark: '',
    fund_detail: {
      fund_type: 'equity',
      manager: '',
      company: '',
      latest_nav: '',
      benchmark: ''
    } as FundDetail
  }
}

function openCreate() {
  form.value = emptyForm()
  isEdit.value = false
  formVisible.value = true
}

function openEdit(a: Asset) {
  form.value = JSON.parse(JSON.stringify(a))
  if (!form.value.fund_detail) form.value.fund_detail = {} as FundDetail
  isEdit.value = true
  formVisible.value = true
}

async function submitForm() {
  if (!form.value.asset_code || !form.value.name) {
    ElMessage.warning('代码与名称必填')
    return
  }
  try {
    if (isEdit.value && form.value.id) {
      await assetApi.update(form.value.id, form.value)
      ElMessage.success('更新成功')
    } else {
      await assetApi.create(form.value)
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
    await ElMessageBox.confirm(`确定删除基金 ${a.name}（${a.asset_code}）？`, '确认', {
      type: 'warning'
    })
    await assetApi.remove(a.id)
    ElMessage.success('已删除')
    fetchList()
  } catch {
    /* 取消 */
  }
}

// === 刷新净值 ===
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
    /* 全局拦截器已弹错误 */
  } finally {
    refreshing.value = false
  }
}

// === 流水录入 ===
const txnDialog = ref(false)
const txnAsset = ref<Asset | null>(null)
function openTxn(a: Asset) {
  txnAsset.value = a
  txnDialog.value = true
}
function onTxnSubmitted() {
  // 流水入库后，可选择刷新基金最新净值
  fetchList()
}

const fundTypeMap: Record<string, string> = {
  equity: '股票型',
  bond: '债券型',
  hybrid: '混合型',
  money: '货币型',
  index: '指数型',
  qdii: 'QDII'
}

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
        <el-input
          v-model="filter.keyword"
          placeholder="搜索代码/名称"
          style="width: 260px;"
          clearable
          @clear="fetchList"
          @keyup.enter="fetchList"
        />
        <el-select v-model="filter.status" placeholder="状态" clearable style="width: 140px;" @change="fetchList">
          <el-option label="active" value="active" />
          <el-option label="delisted" value="delisted" />
        </el-select>
        <el-button type="primary" @click="fetchList">查询</el-button>
        <div class="fv-grow" />
        <el-button :icon="Refresh" :loading="refreshing" @click="refreshAll">刷新净值</el-button>
        <el-button type="primary" :icon="Plus" @click="openCreate">新增基金</el-button>
      </div>

      <el-table :data="list" v-loading="loading" stripe border :max-height="540">
        <el-table-column prop="asset_code" label="基金代码" width="100" />
        <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small" type="info">{{ fundTypeMap[row.fund_detail?.fund_type] || row.fund_detail?.fund_type || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="fund_detail.company" label="基金公司" width="160" show-overflow-tooltip />
        <el-table-column prop="fund_detail.manager" label="经理" width="100" />
        <el-table-column label="最新净值" width="110" align="right">
          <template #default="{ row }">
            <span>{{ row.fund_detail?.latest_nav || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="净值日" width="110">
          <template #default="{ row }">{{ row.fund_detail?.latest_nav_date?.slice(0, 10) || '-' }}</template>
        </el-table-column>
        <el-table-column label="发行平台" width="140">
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
    <el-dialog v-model="formVisible" :title="isEdit ? '编辑基金' : '新增基金'" width="720px" destroy-on-close>
      <el-form :model="form" label-width="100px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="基金代码" required>
              <el-input v-model="form.asset_code" placeholder="例如 110022" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="基金类型">
              <el-select v-model="form.fund_detail!.fund_type">
                <el-option label="股票型" value="equity" />
                <el-option label="债券型" value="bond" />
                <el-option label="混合型" value="hybrid" />
                <el-option label="货币型" value="money" />
                <el-option label="指数型" value="index" />
                <el-option label="QDII" value="qdii" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="例如 易方达消费行业" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="基金公司">
              <el-input v-model="form.fund_detail!.company" placeholder="例如 易方达基金" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="基金经理">
              <el-input v-model="form.fund_detail!.manager" placeholder="例如 萧楠" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="最新净值">
              <MoneyInput v-model="form.fund_detail!.latest_nav as string" placeholder="例如 2.6512" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="净值日期">
              <el-date-picker v-model="form.fund_detail!.latest_nav_date" type="date" value-format="YYYY-MM-DD" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="发行平台">
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
        <el-form-item label="业绩基准">
          <el-input v-model="form.fund_detail!.benchmark" placeholder="例如 沪深300指数收益率×95%+银行活期存款利率（税后）×5%" />
        </el-form-item>
        <el-form-item label="风险等级">
          <el-select v-model="form.risk_level" clearable>
            <el-option label="R1" value="R1" />
            <el-option label="R2" value="R2" />
            <el-option label="R3" value="R3" />
            <el-option label="R4" value="R4" />
            <el-option label="R5" value="R5" />
          </el-select>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm">{{ isEdit ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <!-- 流水录入 -->
    <TxnDialog
      v-model="txnDialog"
      :asset-id="txnAsset?.id"
      :asset-code="txnAsset?.asset_code"
      :asset-name="txnAsset?.name"
      asset-type="fund"
      :currency="txnAsset?.currency || 'CNY'"
      :allowed-types="['buy', 'sell', 'dividend', 'dividend_reinvest']"
      default-txn-type="buy"
      @submitted="onTxnSubmitted"
    />
  </div>
</template>
