// 对话状态：会话索引（本地持久化）、消息、流式问答
import { defineStore } from 'pinia'
import { chatStream } from '../api/chat'
import type { ChatSource, SessionMeta } from '../api/types'

const SESSIONS_STORAGE = 'binrag_sessions'

// 本地消息：助手消息可携带引用来源与错误标记
export interface LocalMessage {
  role: 'user' | 'assistant'
  content: string
  sources?: ChatSource[]
  error?: boolean
}

function loadSessions(): SessionMeta[] {
  try {
    return JSON.parse(localStorage.getItem(SESSIONS_STORAGE) ?? '[]') as SessionMeta[]
  } catch {
    return []
  }
}

function saveSessions(sessions: SessionMeta[]) {
  localStorage.setItem(SESSIONS_STORAGE, JSON.stringify(sessions))
}

export const useChatStore = defineStore('chat', {
  state: () => ({
    sessions: loadSessions(),
    activeSessionId: '',
    messages: [] as LocalMessage[],
    streaming: false,
    abortController: null as AbortController | null,
  }),

  getters: {
    activeSession(state): SessionMeta | undefined {
      return state.sessions.find((s) => s.id === state.activeSessionId)
    },
  },

  actions: {
    /** 新建会话并激活 */
    newSession(kbId = '') {
      const meta: SessionMeta = {
        id: crypto.randomUUID(),
        title: '新会话',
        kbId,
        updatedAt: new Date().toISOString(),
      }
      this.sessions.unshift(meta)
      saveSessions(this.sessions)
      this.activeSessionId = meta.id
      this.messages = []
      return meta
    },

    /** 切换会话：从后端拉取历史消息 */
    async switchSession(id: string) {
      if (this.streaming) this.stop()
      this.activeSessionId = id
      this.messages = []
      try {
        const resp = await fetch(`/api/v1/chat/history?session_id=${encodeURIComponent(id)}`, {
          headers: { Authorization: `Bearer ${localStorage.getItem('binrag_api_key') ?? ''}` },
        })
        if (resp.ok) {
          const body = await resp.json()
          const msgs = body?.data as Array<{ role: string; content: string }> | undefined
          if (Array.isArray(msgs)) {
            this.messages = msgs.map((m) => ({
              role: m.role === 'user' ? ('user' as const) : ('assistant' as const),
              content: m.content,
            }))
          }
        }
      } catch {
        /* 历史加载失败静默（保留空消息区） */
      }
    },

    /** 删除会话索引（后端历史保留） */
    deleteSession(id: string) {
      this.sessions = this.sessions.filter((s) => s.id !== id)
      saveSessions(this.sessions)
      if (this.activeSessionId === id) {
        this.activeSessionId = ''
        this.messages = []
      }
    },

    /** 发送提问：追加消息 → 流式接收 */
    async send(question: string) {
      if (!question.trim() || this.streaming) return
      const sessionId = this.activeSessionId || this.newSession().id

      this.messages.push({ role: 'user', content: question })
      this.messages.push({ role: 'assistant', content: '', sources: [] })
      this.streaming = true
      this.abortController = new AbortController()

      // 会话标题：首条用户消息前 20 字
      const meta = this.sessions.find((s) => s.id === sessionId)
      if (meta && meta.title === '新会话') {
        meta.title = question.trim().slice(0, 20)
        meta.updatedAt = new Date().toISOString()
        saveSessions(this.sessions)
      }

      const assistantIndex = this.messages.length - 1
      let hasContent = false

      try {
        await chatStream(
          {
            session_id: sessionId,
            question,
            kb_id: this.activeSession?.kbId ?? '',
          },
          (ev) => {
            switch (ev.type) {
              case 'sources':
                this.messages[assistantIndex].sources = ev.sources
                break
              case 'chunk':
                hasContent = true
                this.messages[assistantIndex].content += ev.content
                break
              case 'error':
                this.messages[assistantIndex].error = true
                this.messages[assistantIndex].content = ev.message || '生成回答失败'
                break
              case 'done':
                break
            }
          },
          this.abortController.signal,
        )
      } catch (err) {
        const aborted = (err as Error)?.name === 'AbortError'
        if (!aborted) {
          this.messages[assistantIndex].error = true
          this.messages[assistantIndex].content =
            this.messages[assistantIndex].content || '请求失败，请检查网络或稍后重试'
        }
      } finally {
        this.streaming = false
        this.abortController = null
        if (!hasContent && !this.messages[assistantIndex].error) {
          this.messages[assistantIndex].content = '（无回答）'
        }
        if (meta) {
          meta.updatedAt = new Date().toISOString()
          saveSessions(this.sessions)
        }
      }
    },

    /** 停止当前流式回答 */
    stop() {
      this.abortController?.abort()
    },
  },
})
