import { defineStore } from 'pinia'
import { ref } from 'vue'
import { aiApi } from '@/api/ai'
import type { ProviderInfo } from '@/api/types'

export const useAIStore = defineStore('ai', () => {
  const providers = ref<ProviderInfo[]>([])
  const currentProvider = ref<string>('')

  async function loadProviders() {
    try {
      providers.value = (await aiApi.providers()) || []
      const def = providers.value.find((p) => p.is_default)
      currentProvider.value = def?.name || providers.value[0]?.name || ''
    } catch (e) {
      providers.value = []
    }
  }

  function setProvider(name: string) {
    currentProvider.value = name
  }

  return { providers, currentProvider, loadProviders, setProvider }
})
