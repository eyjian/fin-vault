<script setup lang="ts">
// 现金账户管理
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, Money } from '@element-plus/icons-vue'
import { assetApi } from '@/api/asset'
import { usePlatformStore } from '@/stores/platform'
import type { Asset } from '@/api/types'
import TxnDialog from '@/components/TxnDialog.vue'

const platformStore = usePlatformStore()
const list = ref<Asset[]>([])
const loading = ref(false)
const filter = reactive({ page: 1, page_size: 50 })

async function fetchList() {
  loading.value = true
  try {
    const r = await assetApi.cash(filter)
    list.value = r?.items || r?.list || []
  } catch {
    list.value = []
  } finally {
    loading.value = false
  }
}

const formVisible = ref(false)
const form = ref<Asset>(emptyForm())
function emptyForm(): Asset {
  return {
    asset_code: 'CASH--CNY',
    name: '',
    asset_type: 'cash',
    currency: 'CNY',
    status: 'active',
    issuer_platform_id: undefined
  }
}
function openCreate() {
  form.value = emptyForm()
  formVisible.value = true
}
function rebuildCode() {
  const platformCode = platformStore.platforms.find((p) => p.id === form.value.issuer_platform_id)?.code || ''
  form.value.asset_code = `CASH-${platformCode}-${form.value.currency || 'CNY'}`
}

async function submitForm() {
  rebuildCode()
  if (!form.value.name) form.value.name = `${form.value.currency} 现金`
  try {
    await assetApi.create(form.value)
    ElMessage.success('已创建')
    formVisible.value = false
    fetchList()
  } catch {
    /* */
  }
}

async function remove(a: Asset) {
  if (!a.id) return
  try {
    await ElMessageBox.confirm(`确定删除现金账户 ${a.name}？`, '确认', { type: 'warning' })
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

onMounted(async () => {
  await platformStore.load()
  await fetchList()
})
</script>

<template>
  <div class="fv-page">
    <div class="fv-card">
      <div class="fv-flex" style="margin-bottom: 12px;">
        <div style="color: var(--fv-text-muted); font-size: 13px;">
          现金账户：每个 平台×币种 一个，编码 CASH-{platform}-{currency}
        </div>
        <div class="fv-grow" />
        <el-button type="primary" :icon="Plus" @click="openCreate">新增现金账户</el-button>
      </div>

      <el-table :data="list" v-loading="loading" stripe border :max-height="540">
        <el-table-column prop="asset_code" label="编码" width="220" />
        <el-table-column prop="name" label="名称" />
        <el-table-column label="平台" width="180">
          <template #default="{ row }">{{ platformStore.nameOf(row.issuer_platform_id) || '-' }}</template>
        </el-table-column>
        <el-table-column prop="currency" label="币种" width="80" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Money" @click="openTxn(row)">充提流水</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="formVisible" title="新增现金账户" width="500px" destroy-on-close>
      <el-form :model="form" label-width="80px">
        <el-form-item label="平台">
          <el-select v-model="form.issuer_platform_id" filterable @change="rebuildCode">
            <el-option v-for="p in platformStore.platforms" :key="p.id" :value="p.id" :label="p.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="币种">
          <el-select v-model="form.currency" @change="rebuildCode">
            <el-option label="CNY" value="CNY" />
            <el-option label="HKD" value="HKD" />
            <el-option label="USD" value="USD" />
          </el-select>
        </el-form-item>
        <el-form-item label="编码">
          <el-input v-model="form.asset_code" disabled />
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm">创建</el-button>
      </template>
    </el-dialog>

    <TxnDialog
      v-model="txnDialog"
      :asset-id="txnAsset?.id"
      :asset-code="txnAsset?.asset_code"
      :asset-name="txnAsset?.name"
      asset-type="cash"
      :currency="txnAsset?.currency || 'CNY'"
      :allowed-types="['deposit', 'withdraw', 'interest', 'cash_in', 'cash_out', 'adjust']"
      default-txn-type="deposit"
      @submitted="fetchList"
    />
  </div>
</template>
