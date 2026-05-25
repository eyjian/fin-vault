<script setup lang="ts">
// TxnDialog：交易录入对话框。覆盖 13 种 txn_type；
// 默认按 props.defaultTxnType 预设，金额字段用 MoneyInput。

import { ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { txnApi } from '@/api/transaction'
import type { Transaction, TxnType, AssetType } from '@/api/types'
import MoneyInput from './MoneyInput.vue'
import { usePlatformStore } from '@/stores/platform'
import { toDecimal } from '@/utils/decimal'

interface Props {
  modelValue: boolean
  assetId?: number
  assetCode?: string
  assetName?: string
  assetType?: AssetType
  currency?: string
  // 允许用的 txn_type 列表；默认 buy + sell
  allowedTypes?: TxnType[]
  defaultTxnType?: TxnType
}

const props = withDefaults(defineProps<Props>(), {
  currency: 'CNY',
  allowedTypes: () => ['buy', 'sell'] as TxnType[],
  defaultTxnType: 'buy'
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'submitted', t: Transaction): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v)
})

const platformStore = usePlatformStore()

interface FormState {
  txn_type: TxnType
  txn_time: string
  platform_id: number | undefined
  quantity: string
  price: string
  amount: string
  fee: string
  tax: string
  currency: string
  external_id: string
  note: string
}

const form = ref<FormState>(emptyForm())

function emptyForm(): FormState {
  return {
    txn_type: props.defaultTxnType,
    txn_time: new Date().toISOString(),
    platform_id: undefined,
    quantity: '',
    price: '',
    amount: '',
    fee: '0',
    tax: '0',
    currency: props.currency || 'CNY',
    external_id: '',
    note: ''
  }
}

watch(
  () => props.modelValue,
  (v) => {
    if (v) {
      form.value = emptyForm()
      platformStore.load()
    }
  }
)

// 自动联动：quantity * price -> amount（仅当 amount 为空时）
watch(
  () => [form.value.quantity, form.value.price],
  ([q, p]) => {
    if (form.value.amount) return
    if (q && p) {
      const qd = toDecimal(q)
      const pd = toDecimal(p)
      if (!qd.isZero() && !pd.isZero()) {
        form.value.amount = qd.mul(pd).toFixed(2)
      }
    }
  }
)

const txnLabels: Record<TxnType, string> = {
  buy: '买入',
  sell: '卖出',
  dividend: '现金分红',
  dividend_reinvest: '分红再投',
  split: '拆股',
  bonus: '送股',
  mature: '到期',
  interest: '利息',
  deposit: '充值',
  withdraw: '提现',
  cash_in: '现金入账',
  cash_out: '现金出账',
  adjust: '手动调整'
}

async function submit() {
  if (!props.assetId) {
    ElMessage.warning('请先选择资产')
    return
  }
  if (!form.value.platform_id) {
    ElMessage.warning('请选择平台')
    return
  }
  if (!form.value.quantity || toDecimal(form.value.quantity).isZero()) {
    ElMessage.warning('请填数量')
    return
  }
  if (!form.value.amount || toDecimal(form.value.amount).isZero()) {
    ElMessage.warning('请填金额')
    return
  }
  const payload: Transaction = {
    asset_id: props.assetId,
    platform_id: form.value.platform_id,
    txn_type: form.value.txn_type,
    txn_time: form.value.txn_time,
    quantity: form.value.quantity,
    price: form.value.price || '0',
    amount: form.value.amount,
    fee: form.value.fee || '0',
    tax: form.value.tax || '0',
    currency: form.value.currency,
    source: '手动',
    external_id: form.value.external_id || undefined,
    note: form.value.note || undefined
  }
  try {
    const created = await txnApi.create(payload)
    ElMessage.success('录入成功')
    emit('submitted', created)
    visible.value = false
  } catch (e) {
    // 错误已被全局拦截器弹消息
  }
}
</script>

<template>
  <el-dialog v-model="visible" title="录入交易流水" width="640px" destroy-on-close>
    <div v-if="props.assetCode || props.assetName" style="margin-bottom: 12px;">
      <el-tag type="info">{{ props.assetCode }}</el-tag>
      <span style="margin-left: 8px;">{{ props.assetName }}</span>
    </div>
    <el-form :model="form" label-width="100px">
      <el-form-item label="类型">
        <el-select v-model="form.txn_type">
          <el-option
            v-for="t in props.allowedTypes"
            :key="t"
            :value="t"
            :label="txnLabels[t] || t"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="平台">
        <el-select v-model="form.platform_id" placeholder="请选择平台" filterable>
          <el-option
            v-for="p in platformStore.platforms"
            :key="p.id"
            :value="p.id"
            :label="p.name + ' (' + p.code + ')'"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="交易时间">
        <el-date-picker v-model="form.txn_time" type="datetime" placeholder="选择时间" />
      </el-form-item>
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="数量">
            <MoneyInput v-model="form.quantity" placeholder="例如 100" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="单价">
            <MoneyInput v-model="form.price" placeholder="例如 1.2345" />
          </el-form-item>
        </el-col>
      </el-row>
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="金额">
            <MoneyInput v-model="form.amount" placeholder="自动计算" />
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
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="手续费">
            <MoneyInput v-model="form.fee" placeholder="0.00" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="税费">
            <MoneyInput v-model="form.tax" placeholder="0.00" />
          </el-form-item>
        </el-col>
      </el-row>
      <el-form-item label="外部订单号">
        <el-input v-model="form.external_id" placeholder="可选，用于防重导入" />
      </el-form-item>
      <el-form-item label="备注">
        <el-input v-model="form.note" type="textarea" :rows="2" placeholder="可选" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" @click="submit">提交</el-button>
    </template>
  </el-dialog>
</template>
