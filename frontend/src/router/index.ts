import { createRouter, createWebHistory, RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', component: () => import('@/views/Dashboard.vue'), meta: { title: '总览' } },
  { path: '/fund', component: () => import('@/views/asset/FundManage.vue'), meta: { title: '基金管理' } },
  { path: '/stock', component: () => import('@/views/asset/StockManage.vue'), meta: { title: '股票管理' } },
  { path: '/wealth', component: () => import('@/views/asset/WealthManage.vue'), meta: { title: '理财产品' } },
  { path: '/cash', component: () => import('@/views/asset/CashManage.vue'), meta: { title: '现金账户' } },
  { path: '/holding', component: () => import('@/views/HoldingView.vue'), meta: { title: '持仓视图' } },
  { path: '/transaction', component: () => import('@/views/transaction/TransactionList.vue'), meta: { title: '交易流水' } },
  { path: '/quote', component: () => import('@/views/QuoteManage.vue'), meta: { title: '行情管理' } },
  { path: '/rate', component: () => import('@/views/RateManage.vue'), meta: { title: '汇率维护' } },
  { path: '/ai-chat', component: () => import('@/views/AIChat.vue'), meta: { title: 'AI 对话' } },
  { path: '/export', component: () => import('@/views/ExportPage.vue'), meta: { title: '数据导出' } }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.afterEach((to) => {
  if (to.meta?.title) {
    document.title = `${to.meta.title} · FinVault`
  }
})

export default router
