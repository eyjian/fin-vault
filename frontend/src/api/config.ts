// 后端配置 API（用于设置页读写多 API 服务商配置 + AI 服务商配置等）。
//
// GET /api/v1/config/data_providers → { data_providers: DataProvidersConfig }
// PUT /api/v1/config/data_providers → { data_providers: DataProvidersConfig }
// GET /api/v1/config/ai_providers → { ai_providers: AIProviderConfig[] }
// PUT /api/v1/config/ai_providers → { ai_providers: AIProviderConfig[] }
// GET /api/v1/config/llm_default → { llm_default: string }
// PUT /api/v1/config/llm_default → { llm_default: string }

import { get, put } from './http'
import type { AIProviderConfig, DataProvidersConfig } from './types'

// 后端返回的是 { data_providers: DataProvidersConfig }，get/put 自动拆了外层 data，
// 所以这里拿到的是 { data_providers: ... }
interface DataProvidersResp {
  data_providers: DataProvidersConfig
}

interface AIProvidersResp {
  ai_providers: AIProviderConfig[]
}

interface LLMDefaultResp {
  llm_default: string
}

export const configApi = {
  async getDataProviders(): Promise<DataProvidersConfig> {
    const resp = await get<DataProvidersResp>('/config/data_providers')
    return resp.data_providers
  },
  async updateDataProviders(cfg: DataProvidersConfig): Promise<DataProvidersConfig> {
    const resp = await put<DataProvidersResp>('/config/data_providers', { data_providers: cfg })
    return resp.data_providers
  },

  // AI 服务商配置
  async getAIProviders(): Promise<AIProviderConfig[]> {
    const resp = await get<AIProvidersResp>('/config/ai_providers')
    return resp.ai_providers || []
  },
  async updateAIProviders(providers: AIProviderConfig[]): Promise<AIProviderConfig[]> {
    const resp = await put<AIProvidersResp>('/config/ai_providers', { ai_providers: providers })
    return resp.ai_providers || []
  },

  // LLM 默认服务商
  async getLLMDefault(): Promise<string> {
    const resp = await get<LLMDefaultResp>('/config/llm_default')
    return resp.llm_default
  },
  async updateLLMDefault(defaultProvider: string): Promise<string> {
    const resp = await put<LLMDefaultResp>('/config/llm_default', { llm_default: defaultProvider })
    return resp.llm_default
  }
}
