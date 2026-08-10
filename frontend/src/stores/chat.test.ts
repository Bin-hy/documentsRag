// chatStore 降级状态机单测：mock chatStream 构造各场景，验证消息/错误/流式状态
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import * as chatApi from '../api/chat'
import type { SSEEvent } from '../api/types'

// mock chatStream：由事件序列驱动回调
function mockStream(events: SSEEvent[], opts: { reject?: unknown } = {}) {
  return vi.spyOn(chatApi, 'chatStream').mockImplementation(
    async (_req, onEvent, _signal) => {
      for (const ev of events) {
        onEvent(ev)
        if (ev.type === 'done' || ev.type === 'error') break
      }
      if (opts.reject !== undefined) {
        throw opts.reject
      }
    },
  )
}

describe('chatStore 降级状态机', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('error 事件：消息标记错误、内容为错误信息、streaming 复位', async () => {
    const store = useChatStore()
    store.newSession()
    mockStream([{ type: 'error', message: '生成回答失败：模型超时' }])

    await store.send('问题')

    const last = store.messages[store.messages.length - 1]
    expect(last.error).toBe(true)
    expect(last.content).toBe('生成回答失败：模型超时')
    expect(store.streaming).toBe(false)
  })

  it('用户停止：已输出内容保留、无错误标记、streaming 复位', async () => {
    const store = useChatStore()
    store.newSession()
    const stream = mockStream([{ type: 'chunk', content: '部分内容' }])

    // 让 chatStream 在收到 chunk 后暂停，模拟流式中途 stop
    stream.mockImplementationOnce(async (_req, onEvent, _signal) => {
      onEvent({ type: 'chunk', content: '部分内容' })
      // 不返回 done，等待 abort
      await new Promise((resolve) => setTimeout(resolve, 50))
    })

    const sendPromise = store.send('问题')
    setTimeout(() => store.stop(), 10)
    await sendPromise

    const last = store.messages[store.messages.length - 1]
    expect(last.content).toBe('部分内容')
    expect(last.error).toBeFalsy()
    expect(store.streaming).toBe(false)
  })

  it('网络失败：消息标记错误、显示可读提示', async () => {
    const store = useChatStore()
    store.newSession()
    mockStream([], { reject: new Error('fetch failed') })

    await store.send('问题')

    const last = store.messages[store.messages.length - 1]
    expect(last.error).toBe(true)
    expect(last.content).toBe('请求失败，请检查网络或稍后重试')
    expect(store.streaming).toBe(false)
  })

  it('空流：显示「（无回答）」、无错误标记', async () => {
    const store = useChatStore()
    store.newSession()
    mockStream([{ type: 'done' }])

    await store.send('问题')

    const last = store.messages[store.messages.length - 1]
    expect(last.content).toBe('（无回答）')
    expect(last.error).toBeFalsy()
    expect(store.streaming).toBe(false)
  })

  it('真实 AbortError：chatStream 抛 AbortError 时静默处理、保留内容、不标错误', async () => {
    const store = useChatStore()
    store.newSession()
    // 先收到部分内容，然后 chatStream 因 abort 抛 AbortError（与真实 abort 一致）
    const abortErr = new DOMException('aborted', 'AbortError')
    mockStream([{ type: 'chunk', content: '部分' }], { reject: abortErr })

    await store.send('问题')

    const last = store.messages[store.messages.length - 1]
    expect(last.content).toBe('部分')
    expect(last.error).toBeFalsy()
    expect(store.streaming).toBe(false)
  })

  it('正常流：sources+chunk+done，内容完整、无错误、streaming 复位', async () => {
    const store = useChatStore()
    store.newSession()
    mockStream([
      { type: 'sources', sources: [{ id: 'c1', filename: 'a.md', heading: 'h', score: 0.9 }] },
      { type: 'chunk', content: '你好' },
      { type: 'chunk', content: '世界' },
      { type: 'done' },
    ])

    await store.send('问题')

    const last = store.messages[store.messages.length - 1]
    expect(last.content).toBe('你好世界')
    expect(last.sources).toHaveLength(1)
    expect(last.error).toBeFalsy()
    expect(store.streaming).toBe(false)
  })

  it('thinking 事件：逐步累积到消息 thinking 数组（思维链数据链路）', async () => {
    const store = useChatStore()
    store.newSession()
    mockStream([
      {
        type: 'thinking',
        step: { type: 'routing', label: '', elapsed_ms: 5, data: { complexity: 'simple', strategy: 'rag' } },
      },
      {
        type: 'thinking',
        step: { type: 'retrieval', label: '', elapsed_ms: 60, data: { query: 'q', recalled: 2, method: 'hybrid' } },
      },
      { type: 'chunk', content: '回答' },
      { type: 'done' },
    ])

    await store.send('问题')

    const last = store.messages[store.messages.length - 1]
    expect(last.thinking).toHaveLength(2)
    expect(last.thinking?.[0].type).toBe('routing')
    expect(last.thinking?.[1].type).toBe('retrieval')
    expect(last.content).toBe('回答')
  })

  it('停止按钮状态：发送期间 streaming=true，结束后 false', async () => {
    const store = useChatStore()
    store.newSession()
    let resolveDone!: () => void
    vi.spyOn(chatApi, 'chatStream').mockImplementationOnce(
      async (_req, onEvent, _signal) => {
        await new Promise<void>((resolve) => {
          resolveDone = resolve
        })
        onEvent({ type: 'done' })
      },
    )

    const sendPromise = store.send('问题')
    expect(store.streaming).toBe(true) // 发送中可停止
    resolveDone()
    await sendPromise
    expect(store.streaming).toBe(false) // 结束后恢复发送
  })
})

describe('chatStore 增强模式', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('增强开关开启时请求体携带 enhanced=true', async () => {
    const store = useChatStore()
    store.newSession()
    store.enhanced = true
    const spy = mockStream([{ type: 'done' }])

    await store.send('问题')

    const req = spy.mock.calls[0][0] as import('../api/types').ChatRequest
    expect(req.enhanced).toBe(true)
  })

  it('默认关闭时请求体 enhanced=false', async () => {
    const store = useChatStore()
    store.newSession()
    const spy = mockStream([{ type: 'done' }])

    await store.send('问题')

    const req = spy.mock.calls[0][0] as import('../api/types').ChatRequest
    expect(req.enhanced).toBe(false)
  })

  it('send 携带 kb_id：无会话时用传入 kb 创建会话并请求（后端必填）', async () => {
    const store = useChatStore()
    const spy = mockStream([{ type: 'done' }])

    await store.send('问题', 'kb-1')

    const req = spy.mock.calls[0][0] as import('../api/types').ChatRequest
    expect(req.kb_id).toBe('kb-1')
    expect(store.activeSession?.kbId).toBe('kb-1')
  })

  it('send 优先使用会话绑定的知识库', async () => {
    const store = useChatStore()
    store.newSession('kb-session')
    const spy = mockStream([{ type: 'done' }])

    await store.send('问题', 'kb-other')

    const req = spy.mock.calls[0][0] as import('../api/types').ChatRequest
    expect(req.kb_id).toBe('kb-session')
  })
})
