<script setup lang="ts">
// 数据导出：直接调浏览器下载
import { reactive } from 'vue'
import { Download } from '@element-plus/icons-vue'
import { exportFileURL } from '@/api/export'

const form = reactive({
  format: 'xlsx' as 'xlsx' | 'md',
  scope: 'full' as 'holdings' | 'transactions' | 'full',
  start: '',
  end: ''
})

function doExport() {
  window.open(exportFileURL(form.format, form.scope, form.start, form.end), '_blank')
}
</script>

<template>
  <div class="fv-page">
    <div class="fv-card" style="max-width: 480px;">
      <h3 style="margin-top: 0;">数据导出</h3>
      <p style="color: var(--fv-text-muted); font-size: 13px;">
        浏览器会直接下载。Markdown 适合给 AI/笔记软件用，Excel 适合做表格。
      </p>
      <el-form :model="form" label-width="80px">
        <el-form-item label="格式">
          <el-radio-group v-model="form.format">
            <el-radio-button value="xlsx">Excel</el-radio-button>
            <el-radio-button value="md">Markdown</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="范围">
          <el-radio-group v-model="form.scope">
            <el-radio-button value="holdings">持仓</el-radio-button>
            <el-radio-button value="transactions">流水</el-radio-button>
            <el-radio-button value="full">全部</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="开始" v-if="form.scope !== 'holdings'">
          <el-date-picker v-model="form.start" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item label="结束" v-if="form.scope !== 'holdings'">
          <el-date-picker v-model="form.end" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="Download" @click="doExport">下载</el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>
