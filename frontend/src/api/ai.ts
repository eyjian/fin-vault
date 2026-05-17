import type { PageResp } from './http'
import { get, post } from './http'
import type { AIConversation, AIMessage, AIScene, ProviderInfo } from './types'

export interface CreateConvReq {
  title?: string
  scene: AIScene
  llm_provider?: string
}

export interface RecommendReq {
  target: 'buy_sell' | 'allocation'
  asset_id?: number
  llm_provider?: string
}

export interface ProfitReq {
  period?: string
  display_currency?: string
  llm_provider?: string
}

export interface RecommendOutput {
  content: string
  tool_calls?: { name: string; args: string; result: string }[]
  conversation_id?: number
  usage?: Record<string, number>
}

export const aiApi = {
  providers() {
    return get<ProviderInfo[]>('/ai/providers')
  },
  listConversations(scene?: string) {
    return get<PageResp<AIConversation>>('/ai/conversations', { scene })
  },
  createConversation(req: CreateConvReq) {
    return post<AIConversation>('/ai/conversations', req)
  },
  listMessages(id: number, limit = 50) {
    return get<AIMessage[]>(`/ai/conversations/${id}/messages`, { limit })
  },
  recommend(req: RecommendReq) {
    return post<RecommendOutput>('/ai/advisor/recommend', req)
  },
  analyzeProfit(req: ProfitReq) {
    return post<RecommendOutput>('/ai/analysis/profit', req)
  }
}

// SSE 流式对话：fetch + ReadableStream，不走 axios
export interface StreamReq {
  conversation_id?: number
  scene: AIScene
  content: string
  llm_provider?: string
}

export interface StreamEvent {
  type: 'chunk' | 'tool_call' | 'tool_result' | 'done' | 'error'
  content?: string
  tool_name?: string
  tool_args?: string
  tool_result?: string
  conversation_id?: number
  finish_reason?: string
}

export async function* aiStream(req: StreamReq, signal?: AbortSignal): AsyncGenerator<StreamEvent> {
  const resp = await fetch('/api/v1/ai/chat/stream', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-User-Id': '1',
      Accept: 'text/event-stream'
    },
    body: JSON.stringify(req),
    signal
  })
  if (!resp.ok || !resp.body) {
    throw new Error(`SSE failed: HTTP ${resp.status}`)
  }
  const reader = resp.body.getReader()
  const decoder = new TextDecoder('utf-8')
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let idx
    while ((idx = buffer.indexOf('\n\n')) >= 0) {
      const block = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)
      const line = block.split('\n').find((l) => l.startsWith('data:'))
      if (!line) continue
      const json = line.slice(5).trim()
      if (!json) continue
      try {
        // 后端用标准 json.Marshal 输出，控制字符已被转义（\n 等），
        // 直接 JSON.parse 即可；不要再做任何字符串还原。
        const ev = JSON.parse(json) as StreamEvent
        yield ev
        if (ev.type === 'done' || ev.type === 'error') return
      } catch (e) {
        console.warn('SSE parse failed', json, e)
      }
    }
  }
}
