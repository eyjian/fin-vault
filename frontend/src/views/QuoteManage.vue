<script setup lang="ts">
// 行情管理：批量刷新（自动 / 单源）+ 手动写入
import { ref, onMounted, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Plus } from '@element-plus/icons-vue'
import { quoteApi, type RefreshResult } from '@/api/quote'
import { assetApi } from '@/api/asset'
import type { Asset } from '@/api/types'
import MoneyInput from '@/components/MoneyInput.vue'

const list = ref<Asset[]>([])
const refreshing = ref(false)

const refreshSource = ref<'auto' | 'eastmoney' | 'sina' | 'tencent'>('auto')

async function loadAssets() {
  try {
    const r = await assetApi.list({ page: 1, page_size: 200 })
    list.value = (r?.items || r?.list || []).filter(
      (a: Asset) => a.asset_type === 'fund' || a.asset_type === 'stock'
    )
  } catch {
    list.value = []
  }
}

const lastResult = ref<RefreshResult[]>([])
async function refreshAll() {
  refreshing.value = true
  try {
    const ids = list.value.map((a) => a.id!).filter(Boolean) as number[]
    lastResult.value = (await quoteApi.refresh({ asset_ids: ids, source: refreshSource.value })) || []
    const ok = lastResult.value.filter((r) => r.ok).length
    ElMessage.success(`刷新完成：成功 ${ok} / ${lastResult.value.length}`)
  } finally {
    refreshing.value = false
  }
}

const manualVisible = ref(false)
const manualForm = reactive({
  asset_id: undefined as number | undefined,
  price: '',
  change_pct: '',
  volume: '',
  source: 'manual'
})
async function submitManual() {
  if (!manualForm.asset_id || !manualForm.price) {
    ElMessage.warning('请填资产 ID 与价格')
    return
  }
  await quoteApi.saveManual({
    asset_id: manualForm.asset_id,
    price: manualForm.price,
    change_pct: manualForm.change_pct,
    volume: manualForm.volume,
    source: 'manual'
  })
  ElMessage.success('已写入')
  manualVisible.value = false
}

onMounted(loadAssets)
</script>

<template>
  <div class="fv-page">
    <div class="fv-card">
      <div class="fv-flex" style="margin-bottom: 12px;">
        <el-select v-model="refreshSource" style="width: 160px;">
          <el-option label="自动 (按优先级)" value="auto" />
          <el-option label="东方财富" value="eastmoney" />
          <el-option label="新浪" value="sina" />
          <el-option label="腾讯" value="tencent" />
        </el-select>
        <el-button type="primary" :icon="Refresh" :loading="refreshing" @click="refreshAll">
          刷新全部行情（{{ list.length }} 项）
        </el-button>
        <div class="fv-grow" />
        <el-button :icon="Plus" @click="manualVisible = true">手动写入</el-button>
      </div>

      <div v-if="lastResult.length > 0" style="margin-top: 12px;">
        <el-divider>刷新结果</el-divider>
        <el-table :data="lastResult" stripe border :max-height="400">
          <el-table-column prop="asset_id" label="资产ID" width="90" />
          <el-table-column prop="asset_code" label="资产代码" width="120" />
          <el-table-column prop="name" label="资产名称" min-width="140" show-overflow-tooltip />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="row.ok ? 'success' : 'danger'" size="small">{{ row.ok ? '成功' : '失败' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="source" label="来源" width="140" />
          <el-table-column prop="price" label="价格" align="right" width="130" />
          <el-table-column prop="message" label="消息" show-overflow-tooltip />
        </el-table>
      </div>
    </div>

    <el-dialog v-model="manualVisible" title="手动写入行情" width="500px">
      <el-form :model="manualForm" label-width="100px">
        <el-form-item label="资产">
          <el-select v-model="manualForm.asset_id" placeholder="选择资产" filterable style="width: 100%;">
            <el-option
              v-for="a in list"
              :key="a.id"
              :value="a.id"
              :label="`${a.asset_code} ${a.name}`"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="价格">
          <MoneyInput v-model="manualForm.price" />
        </el-form-item>
        <el-form-item label="涨跌幅 %">
          <MoneyInput v-model="manualForm.change_pct" />
        </el-form-item>
        <el-form-item label="成交量">
          <MoneyInput v-model="manualForm.volume" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="manualVisible = false">取消</el-button>
        <el-button type="primary" @click="submitManual">提交</el-button>
      </template>
    </el-dialog>
  </div>
</template>
