import { defineStore } from 'pinia'
import { ref } from 'vue'
import { platformApi } from '@/api/platform'
import type { Platform } from '@/api/types'

export const usePlatformStore = defineStore('platform', () => {
  const platforms = ref<Platform[]>([])
  const loaded = ref(false)

  async function load(force = false) {
    if (loaded.value && !force) return platforms.value
    try {
      platforms.value = (await platformApi.list()) || []
      loaded.value = true
    } catch (e) {
      // 容错：未启动后端时也能进入页面
      platforms.value = []
    }
    return platforms.value
  }

  function nameOf(id?: number | null) {
    if (!id) return ''
    return platforms.value.find((p) => p.id === id)?.name || ''
  }

  return { platforms, loaded, load, nameOf }
})
