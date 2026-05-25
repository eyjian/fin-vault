<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

const menuItems = [
  { path: '/dashboard', icon: 'DataAnalysis', title: '总览' },
  { path: '/fund', icon: 'Money', title: '基金管理' },
  { path: '/stock', icon: 'TrendCharts', title: '股票管理' },
  { path: '/wealth', icon: 'Wallet', title: '理财产品' },
  { path: '/cash', icon: 'CreditCard', title: '现金账户' },
  { path: '/holding', icon: 'PieChart', title: '持仓视图' },
  { path: '/transaction', icon: 'Tickets', title: '交易流水' },
  { path: '/quote', icon: 'Cpu', title: '行情管理' },
  { path: '/rate', icon: 'Switch', title: '汇率维护' },
  { path: '/ai-chat', icon: 'ChatLineRound', title: 'AI 对话' },
  { path: '/export', icon: 'Download', title: '数据导出' },
  { path: '/settings', icon: 'Setting', title: '设置' }
]

const activeMenu = computed(() => route.path)
</script>

<template>
  <el-container style="height: 100%;">
    <el-aside width="200px" style="background: #20222a; color: #fff;">
      <div style="padding: 18px 16px; font-size: 18px; font-weight: 600;">
        🏦 FinVault
        <div style="font-size: 12px; color: #909399; margin-top: 2px;">锦仓个人理财</div>
      </div>
      <el-menu
        :default-active="activeMenu"
        background-color="#20222a"
        text-color="#cfd3dc"
        active-text-color="#409eff"
        router
      >
        <el-menu-item v-for="m in menuItems" :key="m.path" :index="m.path">
          <el-icon><component :is="m.icon" /></el-icon>
          <template #title>{{ m.title }}</template>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header style="background: #fff; border-bottom: 1px solid var(--fv-border); display: flex; align-items: center; justify-content: space-between;">
        <div style="font-size: 16px; font-weight: 600;">{{ menuItems.find(m => m.path === activeMenu)?.title || 'FinVault' }}</div>
        <div style="color: var(--fv-text-muted); font-size: 12px;">本地单用户模式</div>
      </el-header>
      <el-main>
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>
