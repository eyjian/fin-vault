import type { PageResp } from './http'
import { get, post, del } from './http'
import type { AISession, AIMessage, ProviderInfo, SendResp } from './types'

export const aiApi = {
  providers() {
    return get<ProviderInfo[]>('/ai/providers')
  },
  listSessions(page = 1, pageSize = 20) {
    return get<PageResp<AISession>>('/ai/sessions', { page, page_size: pageSize })
  },
  createSession(title?: string) {
    return post<{ session_id: string; title: string; created_at: string }>('/ai/sessions', title ? { title } : {})
  },
  getSession(id: string) {
    return get<AISession>(`/ai/sessions/${id}`)
  },
  deleteSession(id: string) {
    return del(`/ai/sessions/${id}`)
  },
  listMessages(sessionId: string, limit = 50) {
    return get<AIMessage[]>(`/ai/sessions/${sessionId}/messages`, { limit })
  },
  sendMessage(sessionId: string, content: string) {
    return post<SendResp>(`/ai/sessions/${sessionId}/messages`, { content })
  }
}
