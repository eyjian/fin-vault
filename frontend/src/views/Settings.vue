<script setup lang="ts">
// 设置页：多 API 服务商配置 + AI 服务商配置
import { configApi } from '@/api/config'
import type { AIProviderConfig, DataProvidersConfig } from '@/api/types'
import { ElMessage } from 'element-plus'
import { onMounted, reactive, ref } from 'vue'

const loading = ref(false)
const saving = ref(false)
const aiLoading = ref(false)
const aiSaving = ref(false)

const form = reactive<DataProvidersConfig>({
  tushare: {
    enabled: false,
    token: '',
    base_url: 'https://api.tushare.pro'
  }
})

const aiProviders = reactive<AIProviderConfig[]>([])

const llmDefault = ref('')

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
    ElMessage.success('数据源配置已保存')
  } catch {
    // http.ts 拦截器已弹错误提示
  } finally {
    saving.value = false
  }
}

async function loadAIConfig() {
  aiLoading.value = true
  try {
    const providers = await configApi.getAIProviders()
    aiProviders.splice(0, aiProviders.length, ...(providers || []))
    const def = await configApi.getLLMDefault()
    llmDefault.value = def || ''
  } catch {
    // 加载失败时使用默认值
  } finally {
    aiLoading.value = false
  }
}

async function saveAIConfig() {
  aiSaving.value = true
  try {
    await configApi.updateAIProviders(aiProviders)
    await configApi.updateLLMDefault(llmDefault.value)
    ElMessage.success('AI 服务商配置已保存')
    // 重新加载以获取脱敏后的数据
    await loadAIConfig()
  } catch {
    // http.ts 拦截器已弹错误提示
  } finally {
    aiSaving.value = false
  }
}

function addAIProvider() {
  aiProviders.push({
    name: '',
    enabled: false,
    api_key: '',
    base_url: '',
    model: ''
  })
}

function removeAIProvider(index: number) {
  aiProviders.splice(index, 1)
}

onMounted(() => {
  loadConfig()
  loadAIConfig()
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

    <!-- AI 服务商配置 -->
    <div class="fv-card" style="margin-top: 20px;">
      <h2 style="margin: 0 0 20px 0; font-size: 18px;">AI 服务商设置</h2>

      <el-form label-width="140px" v-loading="aiLoading">
        <el-form-item label="默认 AI 服务商">
          <el-select v-model="llmDefault" placeholder="选择默认 AI 服务商" clearable>
            <el-option
              v-for="p in aiProviders"
              :key="p.name"
              :label="p.name"
              :value="p.name"
              :disabled="!p.enabled"
            />
          </el-select>
        </el-form-item>

        <el-divider content-position="left">AI 服务商列表</el-divider>

        <div v-for="(provider, index) in aiProviders" :key="index" style="margin-bottom: 16px; padding: 12px; border: 1px solid var(--el-border-color-lighter); border-radius: 8px;">
          <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
            <span style="font-weight: 600;">{{ provider.name || '新服务商' }}</span>
            <el-button type="danger" text size="small" @click="removeAIProvider(index)">删除</el-button>
          </div>

          <el-form-item label="服务商标识">
            <el-input v-model="provider.name" placeholder="如：deepseek" />
          </el-form-item>

          <el-form-item label="启用">
            <el-switch v-model="provider.enabled" />
          </el-form-item>

          <el-form-item label="API Key">
            <el-input
              v-model="provider.api_key"
              type="password"
              show-password
              placeholder="输入 API Key"
              :disabled="!provider.enabled"
            />
          </el-form-item>

          <el-form-item label="API 地址">
            <el-input
              v-model="provider.base_url"
              placeholder="自定义 API 地址（可选）"
              :disabled="!provider.enabled"
            />
          </el-form-item>

          <el-form-item label="模型">
            <el-input
              v-model="provider.model"
              placeholder="默认模型名称（可选）"
              :disabled="!provider.enabled"
            />
          </el-form-item>
        </div>

        <el-button type="success" plain @click="addAIProvider" style="width: 100%;">+ 添加 AI 服务商</el-button>

        <el-divider />

        <el-form-item>
          <el-button type="primary" :loading="aiSaving" @click="saveAIConfig">保存 AI 设置</el-button>
          <el-button @click="loadAIConfig">重置</el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>

<style scoped>
.fv-card {
  background: var(--el-bg-color);
  border-radius: 8px;
  padding: 20px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.04);
}
</style>
