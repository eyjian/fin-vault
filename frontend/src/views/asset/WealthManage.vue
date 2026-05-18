<script setup lang="ts">
// 理财产品管理页（用户必交付项）
//   - 录入：理财主表 + WealthDetail（产品类型/起息/到期/预期年化/起购/赎回规则/自动续期）
//   - 列表：名称 / 代码 / 类型 / 预期年化 / 起息 / 到期 / 期限 / 起购 / 平台 / 状态
//   - 即将到期标记：到期日 ≤ 当前 + maturing_within_days
//   - 录流水：买入 / 提前赎回（sell）/ 利息 / 到期（mature 由系统自动生成，不在 UI）

import { assetApi } from '@/api/asset'
import type { Asset, WealthDetail } from '@/api/types'
import MoneyInput from '@/components/MoneyInput.vue'
import TxnDialog from '@/components/TxnDialog.vue'
import { usePlatformStore } from '@/stores/platform'
import { AlarmClock, Delete, Edit, Money, Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

const platformStore = usePlatformStore()
const router = useRouter()

function viewTxn(a: Asset) {
  if (!a.id) return
  window.open(`/transaction?asset_id=${a.id}`, '_blank')
}

const list = ref<Asset[]>([])
const total = ref(0)
const loading = ref(false)
const filter = reactive({
  keyword: '',
  product_type: '',
  status: '',
  maturing_within_days: 0,
  page: 1,
  page_size: 20
})

async function fetchList() {
  loading.value = true
  try {
    const r = await assetApi.wealth({
      page: filter.page,
      page_size: filter.page_size,
      keyword: filter.keyword || undefined,
      status: filter.status || undefined,
      maturing_within_days: filter.maturing_within_days || undefined,
      include_holdings: true
    })
    let arr = r?.items || r?.list || []
    if (filter.product_type) {
      arr = arr.filter((a: Asset) => a.wealth_detail?.product_type === filter.product_type)
    }
    list.value = arr
    total.value = r?.total || 0
  } catch {
    list.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

// === 表单 ===
const formVisible = ref(false)
const isEdit = ref(false)
const form = ref<Asset>(emptyForm())

function emptyForm(): Asset {
  return {
    asset_code: '',
    name: '',
    asset_type: 'wealth',
    currency: 'CNY',
    status: '活跃',
    issuer_platform_id: undefined,
    risk_level: 'R2',
    remark: '',
    wealth_detail: {
      product_type: 'fixed_deposit',
      expected_yield: '',
      term_days: 0,
      min_amount: '',
      redemption_rule: '',
      is_auto_renewal: false
    } as WealthDetail
  }
}

function openCreate() {
  form.value = emptyForm()
  isEdit.value = false
  formVisible.value = true
}

function openEdit(a: Asset) {
  form.value = JSON.parse(JSON.stringify(a))
  if (!form.value.wealth_detail) form.value.wealth_detail = {} as WealthDetail
  isEdit.value = true
  formVisible.value = true
}

function sanitizePayload(a: Asset): Asset {
  const p: any = JSON.parse(JSON.stringify(a))
  if (p.wealth_detail) {
    const wd = p.wealth_detail
    if (wd.expected_yield === '' || wd.expected_yield == null) delete wd.expected_yield
    if (wd.actual_yield === '' || wd.actual_yield == null) delete wd.actual_yield
    if (wd.min_amount === '' || wd.min_amount == null) delete wd.min_amount
    if (!wd.start_date) delete wd.start_date
    if (!wd.end_date) delete wd.end_date
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
  if (!form.value.wealth_detail?.product_type) {
    ElMessage.warning('请选择产品类型')
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
    /* */
  }
}

async function remove(a: Asset) {
  if (!a.id) return
  try {
    await ElMessageBox.confirm(`确定删除理财 ${a.name}（${a.asset_code}）？`, '确认', { type: 'warning' })
    await assetApi.remove(a.id)
    ElMessage.success('已删除')
    fetchList()
  } catch {
    /* */
  }
}

const txnDialog = ref(false)
const txnAsset = ref<Asset | null>(null)
function openTxn(a: Asset) {
  txnAsset.value = a
  txnDialog.value = true
}

// 盈亏格式化辅助函数
function getPnlColor(val: string | number | null | undefined): string {
  if (!val) return ''
  const num = typeof val === 'string' ? parseFloat(val) : val
  if (isNaN(num)) return ''
  if (num > 0) return '#67C23A' // 绿色
  if (num < 0) return '#F56C6C' // 红色
  return ''
}

function formatPnl(val: string | number | null | undefined): string {
  if (!val && val !== 0) return '-'
  const num = typeof val === 'string' ? parseFloat(val) : val
  if (isNaN(num)) return '-'
  const prefix = num > 0 ? '+' : ''
  return prefix + num.toFixed(2)
}

function formatPnlRatio(val: string | number | null | undefined): string {
  if (!val && val !== 0) return '-'
  const num = typeof val === 'string' ? parseFloat(val) : val
  if (isNaN(num)) return '-'
  const prefix = num > 0 ? '+' : ''
  return prefix + (num * 100).toFixed(2) + '%'
}

const productTypeMap: Record<string, string> = {
  fixed_deposit: '定期',
  structured: '结构性',
  floating: '浮动收益',
  pension: '养老'
}

// 即将到期判断
function isMaturingSoon(end?: string | null): boolean {
  if (!end) return false
  const now = Date.now()
  const t = new Date(end).getTime()
  const diff = t - now
  return diff > 0 && diff < 1000 * 60 * 60 * 24 * 30
}
function isMatured(end?: string | null): boolean {
  if (!end) return false
  return new Date(end).getTime() < Date.now()
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
        <el-input v-model="filter.keyword" placeholder="搜索名称/代码" style="width: 220px;" clearable
          @clear="fetchList" @keyup.enter="fetchList" />
        <el-select v-model="filter.product_type" placeholder="产品类型" clearable style="width: 130px;" @change="fetchList">
          <el-option label="定期" value="fixed_deposit" />
          <el-option label="结构性" value="structured" />
          <el-option label="浮动收益" value="floating" />
          <el-option label="养老" value="pension" />
        </el-select>
        <el-input-number
          v-model="filter.maturing_within_days"
          :min="0"
          :max="365"
          placeholder="即将到期(天)"
          style="width: 150px;"
          @change="fetchList"
        />
        <el-select v-model="filter.status" placeholder="状态" clearable style="width: 120px;" @change="fetchList">
          <el-option label="活跃" value="活跃" />
          <el-option label="已到期" value="已到期" />
        </el-select>
        <el-button type="primary" @click="fetchList">查询</el-button>
        <div class="fv-grow" />
        <el-button type="primary" :icon="Plus" @click="openCreate">新增理财</el-button>
      </div>

      <el-table :data="list" v-loading="loading" stripe border :max-height="540">
        <el-table-column prop="asset_code" label="产品代码" width="120" />
        <el-table-column label="名称" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span>{{ row.name }}</span>
            <el-tag v-if="isMatured(row.wealth_detail?.end_date)" type="info" size="small" style="margin-left: 6px;">已到期</el-tag>
            <el-tag v-else-if="isMaturingSoon(row.wealth_detail?.end_date)" type="warning" size="small" style="margin-left: 6px;">
              <el-icon><AlarmClock /></el-icon>
              即将到期
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small" type="info">{{ productTypeMap[row.wealth_detail?.product_type] || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="预期年化" width="100" align="right">
          <template #default="{ row }">{{ row.wealth_detail?.expected_yield ? row.wealth_detail.expected_yield + '%' : '-' }}</template>
        </el-table-column>
        <el-table-column label="起息日" width="110">
          <template #default="{ row }">{{ row.wealth_detail?.start_date?.slice(0,10) || '-' }}</template>
        </el-table-column>
        <el-table-column label="到期日" width="110">
          <template #default="{ row }">{{ row.wealth_detail?.end_date?.slice(0,10) || '-' }}</template>
        </el-table-column>
        <el-table-column label="期限(天)" width="100" align="right" prop="wealth_detail.term_days" />
        <el-table-column label="持有份额" width="100" align="right">
          <template #default="{ row }">{{ row.holding_summary?.quantity || '-' }}</template>
        </el-table-column>
        <el-table-column label="平均成本" width="100" align="right">
          <template #default="{ row }">{{ row.holding_summary?.avg_cost || '-' }}</template>
        </el-table-column>
        <el-table-column label="最新净值" width="100" align="right">
          <template #default="{ row }">{{ row.holding_summary?.latest_price || '-' }}</template>
        </el-table-column>
        <el-table-column label="市值" width="100" align="right">
          <template #default="{ row }">{{ row.holding_summary?.market_value || '-' }}</template>
        </el-table-column>
        <el-table-column label="未实现盈亏" width="120" align="right">
          <template #default="{ row }">
            <span :style="{ color: getPnlColor(row.holding_summary?.unrealized_pnl) }">
              {{ formatPnl(row.holding_summary?.unrealized_pnl) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="总盈亏" width="120" align="right">
          <template #default="{ row }">
            <span :style="{ color: getPnlColor(row.holding_summary?.total_pnl) }">
              {{ formatPnl(row.holding_summary?.total_pnl) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="盈亏比率" width="100" align="right">
          <template #default="{ row }">
            <span :style="{ color: getPnlColor(row.holding_summary?.pnl_ratio) }">
              {{ formatPnlRatio(row.holding_summary?.pnl_ratio) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="已实现盈亏" width="120" align="right">
          <template #default="{ row }">
            <span :style="{ color: getPnlColor(row.holding_summary?.realized_pnl) }">
              {{ formatPnl(row.holding_summary?.realized_pnl) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="累计利息" width="100" align="right">
          <template #default="{ row }">{{ row.holding_summary?.total_dividend || '-' }}</template>
        </el-table-column>
        <el-table-column label="发行平台" width="160">
          <template #default="{ row }">{{ row.wealth_detail?.min_amount || '-' }}</template>
        </el-table-column>
        <el-table-column label="发行平台" width="160">
          <template #default="{ row }">{{ platformName(row.issuer_platform_id) || '-' }}</template>
        </el-table-column>
        <el-table-column label="自动续期" width="90" align="center">
          <template #default="{ row }">{{ row.wealth_detail?.is_auto_renewal ? '是' : '否' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="320" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Money" @click="openTxn(row)">录流水</el-button>
            <el-button size="small" @click="viewTxn(row)">查看流水</el-button>
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

    <!-- 录入 -->
    <el-dialog v-model="formVisible" :title="isEdit ? '编辑理财产品' : '新增理财产品'" width="760px" destroy-on-close>
      <el-form :model="form" label-width="110px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="产品代码" required>
              <el-input v-model="form.asset_code" placeholder="例如 LC202604001" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="产品类型" required>
              <el-select v-model="form.wealth_detail!.product_type">
                <el-option label="定期" value="fixed_deposit" />
                <el-option label="结构性" value="structured" />
                <el-option label="浮动收益" value="floating" />
                <el-option label="养老" value="pension" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="例如 招银理财·稳健 6 个月" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="预期年化(%)">
              <MoneyInput v-model="form.wealth_detail!.expected_yield as string" placeholder="例如 3.85" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="实际年化(%)">
              <MoneyInput v-model="form.wealth_detail!.actual_yield as string" placeholder="到期后填写" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="8">
            <el-form-item label="起息日">
              <el-date-picker v-model="form.wealth_detail!.start_date" type="date" value-format="YYYY-MM-DD" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="到期日">
              <el-date-picker v-model="form.wealth_detail!.end_date" type="date" value-format="YYYY-MM-DD" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="期限(天)">
              <el-input-number v-model="form.wealth_detail!.term_days" :min="0" :max="3650" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="起购金额">
              <MoneyInput v-model="form.wealth_detail!.min_amount as string" placeholder="例如 10000" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="发行平台">
              <el-select v-model="form.issuer_platform_id" placeholder="选择平台" clearable filterable>
                <el-option v-for="p in platformStore.platforms" :key="p.id" :value="p.id" :label="p.name" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="赎回规则">
          <el-input v-model="form.wealth_detail!.redemption_rule" placeholder="例如 持有满 30 天可申请赎回" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="8">
            <el-form-item label="自动续期">
              <el-switch v-model="form.wealth_detail!.is_auto_renewal" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="风险等级">
              <el-select v-model="form.risk_level">
                <el-option label="R1" value="R1" />
                <el-option label="R2" value="R2" />
                <el-option label="R3" value="R3" />
                <el-option label="R4" value="R4" />
                <el-option label="R5" value="R5" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
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
      asset-type="wealth"
      :currency="txnAsset?.currency || 'CNY'"
      :allowed-types="['buy', 'sell', 'interest', 'mature', 'adjust']"
      default-txn-type="buy"
      @submitted="fetchList"
    />
  </div>
</template>
