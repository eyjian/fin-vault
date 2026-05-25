<script setup lang="ts">
// 设置页：多 API 服务商配置
import { configApi } from '@/api/config'
import type { DataProvidersConfig } from '@/api/types'
import { ElMessage } from 'element-plus'
import { onMounted, reactive, ref } from 'vue'

const loading = ref(false)
const saving = ref(false)

const form = reactive<DataProvidersConfig>({
  tushare: {
    enabled: false,
    token: '',
    base_url: 'https://api.tushare.pro'
  }
})

async function loadConfig() {
  loading.value = true
  try {
    const cfg = await configApi.getDataProviders()
    if (cfg?.tushare) {
      form.tushare = cfg.tushare
    }
  } catch {
    // 加载失败时使用默认值
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  try {
    const cfg = await configApi.updateDataProviders(form)
    if (cfg?.tushare) {
      form.tushare = cfg.tushare
    }
    ElMessage.success('配置已保存')
  } catch {
    // http.ts 拦截器已弹错误提示
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadConfig()
})
</script>

<template>
  <div class="fv-page">
    <div class="fv-card">
      <h2 style="margin: 0 0 20px 0; font-size: 18px;">数据源设置</h2>

      <el-form :model="form" label-width="140px" v-loading="loading">
        <el-divider content-position="left">Tushare Pro</el-divider>

        <el-form-item label="启用 Tushare">
          <el-switch v-model="form.tushare.enabled" />
        </el-form-item>

        <el-form-item label="API Token">
          <el-input
            v-model="form.tushare.token"
            type="password"
            show-password
            placeholder="输入 Tushare Pro API Token"
            :disabled="!form.tushare.enabled"
          />
          <div style="margin-top: 4px; font-size: 12px; color: var(--el-text-color-secondary);">
            注册 <el-link href="https://tushare.pro" target="_blank" type="primary" :underline="false">tushare.pro</el-link> 可免费获取 200 积分，
            fund_nav 接口消耗 120 积分，完全覆盖。
          </div>
        </el-form-item>

        <el-form-item label="API 地址">
          <el-input
            v-model="form.tushare.base_url"
            placeholder="https://api.tushare.pro"
            :disabled="!form.tushare.enabled"
          />
        </el-form-item>

        <el-divider />

        <el-form-item>
          <el-button type="primary" :loading="saving" @click="saveConfig">保存设置</el-button>
          <el-button @click="loadConfig">重置</el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>
