import type { RouteComponent, RouteRecordRaw } from 'vue-router'
import { createRouter, createWebHistory } from 'vue-router'

// 动态 import 兜底：当浏览器 fetch chunk 失败时（dev server 重启 / 部署新版本 hash 失效 /
// 网络抖动），自动 reload 一次。用 sessionStorage 防止死循环。
const RELOAD_KEY = '__fv_chunk_reload__'

function lazy(loader: () => Promise<RouteComponent>) {
  return () =>
    loader().catch((err: unknown) => {
      const msg = (err as Error)?.message || String(err)
      const isChunkError =
        /Failed to fetch dynamically imported module/i.test(msg) ||
        /Loading chunk \S+ failed/i.test(msg) ||
        /Importing a module script failed/i.test(msg)
      if (isChunkError && !sessionStorage.getItem(RELOAD_KEY)) {
        sessionStorage.setItem(RELOAD_KEY, '1')
        window.location.reload()
        // 返回一个永不 resolve 的 Promise，避免在刷新过程中 router 抛错
        return new Promise<RouteComponent>(() => {})
      }
      // 刷新过仍失败 → 抛出，让用户看到真实错误
      throw err
    })
}

// 路由守卫：成功导航后清除标记
const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', component: lazy(() => import('@/views/Dashboard.vue')), meta: { title: '总览' } },
  { path: '/fund', component: lazy(() => import('@/views/asset/FundManage.vue')), meta: { title: '基金管理' } },
  { path: '/stock', component: lazy(() => import('@/views/asset/StockManage.vue')), meta: { title: '股票管理' } },
  { path: '/wealth', component: lazy(() => import('@/views/asset/WealthManage.vue')), meta: { title: '理财产品' } },
  { path: '/cash', component: lazy(() => import('@/views/asset/CashManage.vue')), meta: { title: '现金账户' } },
  { path: '/holding', component: lazy(() => import('@/views/HoldingView.vue')), meta: { title: '持仓视图' } },
  { path: '/transaction', component: lazy(() => import('@/views/transaction/TransactionList.vue')), meta: { title: '交易流水' } },
  { path: '/quote', component: lazy(() => import('@/views/QuoteManage.vue')), meta: { title: '行情管理' } },
  { path: '/rate', component: lazy(() => import('@/views/RateManage.vue')), meta: { title: '汇率维护' } },
  { path: '/ai-chat', component: lazy(() => import('@/views/AIChat.vue')), meta: { title: 'AI 对话' } },
  { path: '/export', component: lazy(() => import('@/views/ExportPage.vue')), meta: { title: '数据导出' } }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.afterEach((to) => {
  if (to.meta?.title) {
    document.title = `${to.meta.title} · FinVault`
  }
  // 成功落地任意页面就重置标记，下次再发生 chunk 错误时可以再尝试 reload 一次
  sessionStorage.removeItem(RELOAD_KEY)
})

// 路由本身的 onError（vue-router 4 提供）：例如 push 时 lazy 抛错也能兜住
router.onError((err) => {
  const msg = (err as Error)?.message || String(err)
  if (
    /Failed to fetch dynamically imported module/i.test(msg) ||
    /Loading chunk \S+ failed/i.test(msg)
  ) {
    if (!sessionStorage.getItem(RELOAD_KEY)) {
      sessionStorage.setItem(RELOAD_KEY, '1')
      window.location.reload()
    }
  }
})

export default router
