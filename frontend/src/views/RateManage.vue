<script setup lang="ts">
// 汇率维护：查询 + 录入
import { ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Plus } from '@element-plus/icons-vue'
import { rateApi } from '@/api/rate'
import type { ExchangeRate } from '@/api/types'
import MoneyInput from '@/components/MoneyInput.vue'

const filter = reactive({
  from: 'USD',
  to: 'CNY',
  start: '',
  end: ''
})
const list = ref<ExchangeRate[]>([])
const latest = ref<ExchangeRate | null>(null)
const loading = ref(false)

async function fetchLatest() {
  loading.value = true
  try {
    latest.value = await rateApi.latest(filter.from, filter.to)
  } catch {
    latest.value = null
  } finally {
    loading.value = false
  }
}

async function fetchList() {
  loading.value = true
  try {
    list.value = (await rateApi.list(filter.from, filter.to, filter.start, filter.end)) || []
  } catch {
    list.value = []
  } finally {
    loading.value = false
  }
}

const formVisible = ref(false)
const form = reactive<ExchangeRate>({
  from_currency: 'USD',
  to_currency: 'CNY',
  rate: '',
  quote_date: new Date().toISOString().slice(0, 10),
  source: 'manual'
})

async function submit() {
  if (!form.from_currency || !form.to_currency || !form.rate) {
    ElMessage.warning('必填项缺失')
    return
  }
  await rateApi.save({ ...form })
  ElMessage.success('已写入')
  formVisible.value = false
  fetchList()
  fetchLatest()
}
</script>

<template>
  <div class="fv-page">
    <div class="fv-card">
      <div class="fv-flex" style="margin-bottom: 12px;">
        <el-input v-model="filter.from" style="width: 120px;" placeholder="From" />
        <el-input v-model="filter.to" style="width: 120px;" placeholder="To" />
        <el-date-picker v-model="filter.start" type="date" placeholder="开始" value-format="YYYY-MM-DD" />
        <el-date-picker v-model="filter.end" type="date" placeholder="结束" value-format="YYYY-MM-DD" />
        <el-button type="primary" :icon="Search" @click="fetchList">查询</el-button>
        <el-button @click="fetchLatest">查最新</el-button>
        <div class="fv-grow" />
        <el-button :icon="Plus" type="primary" @click="formVisible = true">录入汇率</el-button>
      </div>

      <el-alert v-if="latest" type="info" :title="`${latest.from_currency} → ${latest.to_currency} 最新汇率：${latest.rate}（${latest.quote_date}, 源 ${latest.source}）`" show-icon style="margin-bottom: 12px;" />

      <el-table :data="list" v-loading="loading" stripe border :max-height="540">
        <el-table-column prop="quote_date" label="日期" width="140">
          <template #default="{ row }">{{ row.quote_date?.slice(0, 10) }}</template>
        </el-table-column>
        <el-table-column prop="from_currency" label="From" width="100" />
        <el-table-column prop="to_currency" label="To" width="100" />
        <el-table-column prop="rate" label="汇率" align="right" />
        <el-table-column prop="source" label="来源" width="120" />
      </el-table>
    </div>

    <el-dialog v-model="formVisible" title="录入汇率" width="480px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="From">
          <el-input v-model="form.from_currency" />
        </el-form-item>
        <el-form-item label="To">
          <el-input v-model="form.to_currency" />
        </el-form-item>
        <el-form-item label="汇率">
          <MoneyInput v-model="form.rate" />
        </el-form-item>
        <el-form-item label="日期">
          <el-date-picker v-model="form.quote_date" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item label="来源">
          <el-select v-model="form.source">
            <el-option label="manual" value="manual" />
            <el-option label="pboc" value="pboc" />
            <el-option label="api" value="api" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" @click="submit">提交</el-button>
      </template>
    </el-dialog>
  </div>
</template>
