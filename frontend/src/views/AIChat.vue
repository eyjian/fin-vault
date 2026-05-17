<script setup lang="ts">
// AI 对话页：Session 管理 + Provider 切换 + 非流式对话

import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Promotion, ChatLineSquare, Delete } from '@element-plus/icons-vue'
import { aiApi } from '@/api/ai'
import { useAIStore } from '@/stores/ai'
import type { ToolCallDTO } from '@/api/types'

const aiStore = useAIStore()

interface ChatLine {
  role: 'user' | 'assistant' | 'tool'
  content: string
  tool_name?: string
  pending?: boolean
}

const messages = ref<ChatLine[]>([])
const input = ref('')
const sessionId = ref<string | undefined>(undefined)
const sending = ref(false)
const loadingHistory = ref(false)

const scrollEl = ref<HTMLElement | null>(null)

async function send() {
  const text = input.value.trim()
  if (!text || sending.value) return
  input.value = ''
  messages.value.push({ role: 'user', content: text })
  const assistantLine: ChatLine = { role: 'assistant', content: '', pending: true }
  messages.value.push(assistantLine)
  sending.value = true
  await scrollToBottom()

  try {
    // 首次发消息时创建 session
    if (!sessionId.value) {
      const created = await aiApi.createSession()
      sessionId.value = created.session_id
    }

    const resp = await aiApi.sendMessage(sessionId.value, text)

    // 填充助手回复
    assistantLine.content = resp.assistant_message?.content || ''
    assistantLine.pending = false

    // 展示工具调用
    for (const tc of resp.tool_calls || []) {
      messages.value.push({
        role: 'tool',
        content: formatToolCall(tc),
        tool_name: tc.name
      })
    }

    await scrollToBottom()
  } catch (e: any) {
    assistantLine.pending = false
    assistantLine.content += `\n\n[错误] ${e?.message || e}`
    ElMessage.error('对话失败')
  } finally {
    sending.value = false
  }
}

function formatToolCall(tc: ToolCallDTO): string {
  const argsStr = tc.arguments ? JSON.stringify(tc.arguments) : ''
  const statusTag = tc.status === 'failed' ? ` ❌ ${tc.error_message || ''}` : ''
  return `${tc.name}(${argsStr})${statusTag}`
}

function newConversation() {
  sessionId.value = undefined
  messages.value = []
}

async function loadHistory() {
  if (!sessionId.value) return
  loadingHistory.value = true
  try {
    const msgs = await aiApi.listMessages(sessionId.value)
    messages.value = msgs.map((m) => ({
      role: m.role as 'user' | 'assistant',
      content: m.content
    }))
    await scrollToBottom()
  } catch {
    // 静默忽略历史加载失败
  } finally {
    loadingHistory.value = false
  }
}

async function deleteSession() {
  if (!sessionId.value) return
  try {
    await aiApi.deleteSession(sessionId.value)
  } catch {
    // 静默
  }
  newConversation()
}

async function scrollToBottom() {
  await new Promise((r) => requestAnimationFrame(r))
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
        <el-select
          v-model="aiStore.currentProvider"
          placeholder="模型"
          style="width: 220px; margin-right: 12px;"
          :disabled="sending"
        >
          <el-option
            v-for="p in aiStore.providers"
            :key="p.name"
            :value="p.name"
            :label="`${p.name} (${p.model})`"
            :disabled="!p.enabled"
          />
        </el-select>
        <div class="fv-grow" />
        <el-button :icon="ChatLineSquare" @click="newConversation" :disabled="sending">新对话</el-button>
        <el-button v-if="sessionId" type="danger" :icon="Delete" @click="deleteSession" :disabled="sending">删除</el-button>
      </div>

      <div ref="scrollEl" style="flex: 1; overflow-y: auto; padding: 8px; background: #fafafa; border-radius: 6px;">
        <div v-if="messages.length === 0" style="color: var(--fv-text-muted); text-align: center; padding: 60px 0;">
          输入问题开始对话，AI 助手会自动调用工具读取真实数据。
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
          <div>{{ m.content }}<span v-if="m.pending" style="color: var(--fv-text-muted);">思考中...</span></div>
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
