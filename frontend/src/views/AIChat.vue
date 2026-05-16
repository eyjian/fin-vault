<script setup lang="ts">
// AI 对话页：4 场景切换 + Provider 切换 + SSE 流式

import { ref, onMounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Promotion, ChatLineSquare, Delete } from '@element-plus/icons-vue'
import { aiApi, aiStream } from '@/api/ai'
import { useAIStore } from '@/stores/ai'
import type { AIScene } from '@/api/types'

const aiStore = useAIStore()

const scene = ref<AIScene>('chat')
const sceneOptions = [
  { value: 'chat', label: '自由问答' },
  { value: 'analysis', label: '盈亏分析' },
  { value: 'buy_sell', label: '买卖建议' },
  { value: 'advisor', label: '持仓建议' }
] as const

interface ChatLine {
  role: 'user' | 'assistant' | 'tool'
  content: string
  tool_name?: string
  pending?: boolean
}

const messages = ref<ChatLine[]>([])
const input = ref('')
const conversationId = ref<number | undefined>(undefined)
const sending = ref(false)
let abort: AbortController | null = null

const scrollEl = ref<HTMLElement | null>(null)

async function send() {
  const text = input.value.trim()
  if (!text || sending.value) return
  input.value = ''
  messages.value.push({ role: 'user', content: text })
  const assistantLine: ChatLine = { role: 'assistant', content: '', pending: true }
  messages.value.push(assistantLine)
  sending.value = true
  abort = new AbortController()
  await scrollToBottom()
  try {
    const it = aiStream(
      {
        conversation_id: conversationId.value,
        scene: scene.value,
        content: text,
        llm_provider: aiStore.currentProvider || undefined
      },
      abort.signal
    )
    for await (const ev of it) {
      if (ev.type === 'chunk' && ev.content) {
        assistantLine.content += ev.content
      } else if (ev.type === 'tool_call') {
        messages.value.push({
          role: 'tool',
          content: `调用工具 ${ev.tool_name}：${ev.tool_args}`,
          tool_name: ev.tool_name
        })
      } else if (ev.type === 'tool_result') {
        messages.value.push({
          role: 'tool',
          content: `${ev.tool_name} 结果：${(ev.tool_result || '').slice(0, 600)}`,
          tool_name: ev.tool_name
        })
      } else if (ev.type === 'done') {
        assistantLine.pending = false
        if (ev.conversation_id) conversationId.value = ev.conversation_id
      } else if (ev.type === 'error') {
        assistantLine.pending = false
        assistantLine.content = (assistantLine.content || '') + '\n\n[错误] ' + (ev.content || '')
      }
      await scrollToBottom()
    }
  } catch (e: any) {
    assistantLine.pending = false
    assistantLine.content += `\n\n[网络错误] ${e?.message || e}`
    ElMessage.error('对话失败')
  } finally {
    sending.value = false
    abort = null
  }
}

function newConversation() {
  conversationId.value = undefined
  messages.value = []
}

function stop() {
  abort?.abort()
}

async function scrollToBottom() {
  await nextTick()
  if (scrollEl.value) {
    scrollEl.value.scrollTop = scrollEl.value.scrollHeight
  }
}

onMounted(async () => {
  await aiStore.loadProviders()
})
</script>

<template>
  <div class="fv-page">
    <div class="fv-card" style="display: flex; flex-direction: column; height: calc(100vh - 120px);">
      <div class="fv-flex" style="margin-bottom: 12px;">
        <el-radio-group v-model="scene" :disabled="sending">
          <el-radio-button v-for="s in sceneOptions" :key="s.value" :value="s.value">{{ s.label }}</el-radio-button>
        </el-radio-group>
        <el-select
          v-model="aiStore.currentProvider"
          placeholder="模型"
          style="width: 220px; margin-left: 12px;"
          :disabled="sending"
        >
          <el-option
            v-for="p in aiStore.providers"
            :key="p.name"
            :value="p.name"
            :label="`${p.name} (${p.model})`"
          />
        </el-select>
        <div class="fv-grow" />
        <el-button :icon="ChatLineSquare" @click="newConversation" :disabled="sending">新对话</el-button>
        <el-button v-if="sending" type="danger" :icon="Delete" @click="stop">停止</el-button>
      </div>

      <div ref="scrollEl" style="flex: 1; overflow-y: auto; padding: 8px; background: #fafafa; border-radius: 6px;">
        <div v-if="messages.length === 0" style="color: var(--fv-text-muted); text-align: center; padding: 60px 0;">
          选择场景，输入问题开始对话。盈亏分析 / 买卖建议 / 持仓建议 会自动调用工具读取真实数据。
        </div>
        <div
          v-for="(m, i) in messages"
          :key="i"
          :class="m.role"
          style="margin-bottom: 12px; padding: 10px 14px; border-radius: 6px;"
          :style="{
            background: m.role === 'user' ? '#ecf5ff' : m.role === 'tool' ? '#fdf6ec' : '#fff',
            border: '1px solid var(--fv-border)',
            whiteSpace: 'pre-wrap',
            lineHeight: 1.7
          }"
        >
          <div style="font-size: 12px; color: var(--fv-text-muted); margin-bottom: 4px;">
            {{ m.role === 'user' ? '我' : m.role === 'tool' ? `工具 / ${m.tool_name || ''}` : 'AI 助手' }}
          </div>
          <div>{{ m.content }}<span v-if="m.pending" style="color: var(--fv-text-muted);">▍</span></div>
        </div>
      </div>

      <div class="fv-flex" style="margin-top: 12px;">
        <el-input
          v-model="input"
          type="textarea"
          :rows="2"
          placeholder="按 Ctrl/Cmd + Enter 发送"
          @keydown.ctrl.enter="send"
          @keydown.meta.enter="send"
        />
        <el-button type="primary" :icon="Promotion" :loading="sending" @click="send">发送</el-button>
      </div>
    </div>
  </div>
</template>
