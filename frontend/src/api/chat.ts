// SSE 流式问答客户端
// 使用 fetch + ReadableStream 手写解析（EventSource 无法携带 Authorization 头，
// 也无法用 AbortController 主动停止）。
import type { ChatRequest, ChatSource, SSEEvent, ThinkingStep } from './types'
import { getStoredApiKey, getStoredToken } from './client'

// 会话 JWT 优先 / API Key 兜底（SSE 不走 axios 拦截器，需手动附加）
function bearerCredential(): string {
  return getStoredToken() || getStoredApiKey()
}

/**
 * 发起流式问答。
 * @param req 问答请求（session_id / question / kb_id）
 * @param onEvent 事件回调：sources / chunk / done / error
 * @param signal 用于停止流式的 AbortSignal
 */
export async function chatStream(
  req: ChatRequest,
  onEvent: (e: SSEEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const resp = await fetch('/api/v1/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      Authorization: `Bearer ${bearerCredential()}`,
    },
    body: JSON.stringify(req),
    signal,
  })

  if (!resp.ok || !resp.body) {
    let message = `请求失败（HTTP ${resp.status}）`
    try {
      const body = await resp.json()
      message = body?.message ?? message
    } catch {
      /* 忽略非 JSON 响应 */
    }
    throw new Error(message)
  }

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let currentEvent = ''

  // 逐行解析 SSE：event: <type>\ndata: <json>\n\n（gin SSEvent 格式）
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    let newlineIndex: number
    while ((newlineIndex = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, newlineIndex).replace(/\r$/, '')
      buffer = buffer.slice(newlineIndex + 1)

      if (line.startsWith('event:')) {
        currentEvent = line.slice(6).trim()
        continue
      }
      if (!line.startsWith('data:')) continue

      const payload = line.slice(5).trim()
      if (!payload) continue

      let ev: SSEEvent
      try {
        ev = toEvent(currentEvent, JSON.parse(payload))
      } catch {
        continue // 非 JSON 的 data 行跳过
      }
      onEvent(ev)
      if (ev.type === 'done') return
    }
  }
}

// toEvent 把 gin SSE 的（事件名 + data JSON）映射为统一事件结构
function toEvent(eventName: string, data: unknown): SSEEvent {
  switch (eventName) {
    case 'thinking':
      return { type: 'thinking', step: data as ThinkingStep }
    case 'sources':
      return { type: 'sources', sources: (data as ChatSource[]) ?? [] }
    case 'chunk': {
      const content = (data as { content?: string })?.content ?? ''
      return { type: 'chunk', content }
    }
    case 'error':
      return { type: 'error', message: (data as { message?: string })?.message ?? '未知错误' }
    case 'done':
    default:
      return { type: 'done' }
  }
}
