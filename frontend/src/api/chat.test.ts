// chatStream SSE 解析单测：用 mock fetch 构造各场景，验证事件解析与错误处理
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { chatStream } from './chat'
import type { SSEEvent } from './types'

// mock fetch：由 SSE 行数组构造 ReadableStream 响应
function mockFetchSSE(lines: string[], init: { ok?: boolean; status?: number } = {}) {
  const ok = init.ok ?? true
  const status = init.status ?? 200
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(lines.join('\n')))
      controller.close()
    },
  })
  return { ok, status, body } as unknown as Response
}

// mock fetch：返回 JSON body（非 2xx 场景）
function mockFetchJSON(status: number, data: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => data,
    body: null,
  } as unknown as Response
}

describe('chatStream SSE 解析', () => {
  const originalFetch = globalThis.fetch
  const req = { session_id: 's1', question: 'q', kb_id: '' }

  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    globalThis.fetch = originalFetch
  })

  it('正常流：sources → chunk×2 → done 顺序解析', async () => {
    const lines = [
      'event: sources',
      'data: [{"id":"c1","filename":"a.md","heading":"h","score":0.9}]',
      '',
      'event: chunk',
      'data: {"content":"你好"}',
      '',
      'event: chunk',
      'data: {"content":"世界"}',
      '',
      'event: done',
      'data: {}',
      '',
    ]
    vi.mocked(fetch).mockResolvedValue(mockFetchSSE(lines))

    const events: SSEEvent[] = []
    await chatStream(req, (e) => events.push(e))

    expect(events.map((e) => e.type)).toEqual(['sources', 'chunk', 'chunk', 'done'])
    expect(events[0]).toMatchObject({ type: 'sources', sources: [{ id: 'c1', filename: 'a.md' }] })
    expect(events[1]).toMatchObject({ type: 'chunk', content: '你好' })
    expect(events[2]).toMatchObject({ type: 'chunk', content: '世界' })
  })

  it('error 事件：返回 error 事件与 message', async () => {
    const lines = [
      'event: error',
      'data: {"message":"生成失败：模型超时"}',
      '',
    ]
    vi.mocked(fetch).mockResolvedValue(mockFetchSSE(lines))

    const events: SSEEvent[] = []
    await chatStream(req, (e) => events.push(e))

    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({ type: 'error', message: '生成失败：模型超时' })
  })

  it('非 JSON data 行：跳过不抛错，后续事件仍解析', async () => {
    const lines = [
      'event: chunk',
      'data: {"content":"正常"}',
      '',
      'event: chunk',
      'data: 这不是JSON',
      '',
      'event: chunk',
      'data: {"content":"后续"}',
      '',
      'event: done',
      'data: {}',
      '',
    ]
    vi.mocked(fetch).mockResolvedValue(mockFetchSSE(lines))

    const events: SSEEvent[] = []
    await chatStream(req, (e) => events.push(e))

    const chunks = events.filter((e) => e.type === 'chunk')
    expect(chunks.map((c) => (c as { content: string }).content)).toEqual(['正常', '后续'])
  })

  it('HTTP 非 2xx：抛出 Error 且 message 取自后端 message', async () => {
    vi.mocked(fetch).mockResolvedValue(mockFetchJSON(500, { code: 500, message: '问答失败: 内部错误' }))

    await expect(chatStream(req, vi.fn())).rejects.toThrow('问答失败: 内部错误')
  })

  it('HTTP 非 2xx 且无 JSON body：回退 HTTP 状态码', async () => {
    const resp = {
      ok: false,
      status: 502,
      json: async () => {
        throw new Error('not json')
      },
      body: null,
    } as unknown as Response
    vi.mocked(fetch).mockResolvedValue(resp)

    await expect(chatStream(req, vi.fn())).rejects.toThrow('请求失败（HTTP 502）')
  })

  it('中止：传入已 abort 的 signal 抛 AbortError', async () => {
    const controller = new AbortController()
    controller.abort()
    vi.mocked(fetch).mockRejectedValue(new DOMException('aborted', 'AbortError'))

    await expect(chatStream(req, vi.fn(), controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
  })
})
