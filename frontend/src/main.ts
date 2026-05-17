import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import { createPinia } from 'pinia'
import { createApp } from 'vue'

import App from './App.vue'
import router from './router'

import './styles/main.css'

const app = createApp(App)

// 全局错误处理：兜底捕获 lazy chunk 加载失败（router 已经处理大多数情况，
// 这里再加一道，确保任何动态 import 失败都不会让用户卡在白屏）
const CHUNK_RELOAD_KEY = '__fv_chunk_reload__'

function isChunkLoadError(err: unknown): boolean {
  const msg = (err as Error)?.message || String(err)
  return (
    /Failed to fetch dynamically imported module/i.test(msg) ||
    /Loading chunk \S+ failed/i.test(msg) ||
    /Importing a module script failed/i.test(msg)
  )
}

function tryReloadOnce(err: unknown) {
  if (!isChunkLoadError(err)) return false
  if (sessionStorage.getItem(CHUNK_RELOAD_KEY)) return false
  sessionStorage.setItem(CHUNK_RELOAD_KEY, '1')
  window.location.reload()
  return true
}

app.config.errorHandler = (err) => {
  if (tryReloadOnce(err)) return
  console.error('[FinVault] Vue error:', err)
}

window.addEventListener('unhandledrejection', (ev) => {
  if (tryReloadOnce(ev.reason)) {
    ev.preventDefault()
  }
})

app.use(createPinia())
app.use(router)
app.use(ElementPlus)

app.mount('#app')
