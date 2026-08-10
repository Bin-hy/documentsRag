// ThinkingPanel 渲染测试：验证折叠头、时间线步骤、各类型数据区与 active 状态
import { describe, it, expect } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import ThinkingPanel from './ThinkingPanel.vue'
import type { ThinkingStep } from '../api/types'

const steps: ThinkingStep[] = [
  {
    type: 'routing',
    label: '',
    elapsed_ms: 12,
    data: { complexity: 'medium', strategy: 'rag', reasoning: '问题较复杂，需要检索' },
  },
  {
    type: 'retrieval',
    label: '',
    elapsed_ms: 88,
    data: { query: '测试问题', recalled: 3, method: 'hybrid' },
  },
  {
    type: 'rerank',
    label: '',
    elapsed_ms: 30,
    data: {
      query: 'x',
      before: [{ id: '1', filename: 'a.md', score: 0.9, rank: 1 }],
      after: [{ id: '1', filename: 'a.md', score: 0.92, rank: 1 }],
    },
  },
  {
    type: 'chunks',
    label: '',
    elapsed_ms: 5,
    data: { chunks: [{ id: 'c1', filename: 'a.md', heading: '', score: 0.91, content: '目标片段内容……' }] },
  },
  {
    type: 'tool',
    label: '',
    elapsed_ms: 200,
    data: { name: 'web_search', items: [{ title: '示例结果', url: 'https://example.com', snippet: '摘要' }] },
  },
]

describe('ThinkingPanel', () => {
  it('折叠态渲染标题、环节数与状态圆点', () => {
    const w = mount(ThinkingPanel, { props: { steps } })
    expect(w.text()).toContain('思考过程')
    expect(w.text()).toContain('5 环节')
    expect(w.text()).toContain('12ms')
    expect(w.find('.status-orb').exists()).toBe(true)
  })

  it('active 时显示当前环节名（最后一步）', () => {
    const w = mount(ThinkingPanel, { props: { steps, active: true } })
    // currentLabel = 最后一步 labelOf → tool 映射为「工具调用」
    expect(w.text()).toContain('工具调用')
    expect(w.find('.status-orb').classes()).toContain('pulsing')
  })

  it('展开后渲染各环节数据区', async () => {
    const w = mount(ThinkingPanel, { props: { steps } })
    await w.find('.thinking-header').trigger('click')
    const text = w.text()
    expect(text).toContain('路由判定')
    expect(text).toContain('medium')
    expect(text).toContain('问题较复杂，需要检索')
    expect(text).toContain('检索')
    expect(text).toContain('重排前')
    expect(text).toContain('目标片段')
    expect(text).toContain('web_search')
    expect(text).toContain('示例结果')
  })

  it('展开后时间线渲染与步骤数一致', async () => {
    const w = mount(ThinkingPanel, { props: { steps } })
    await w.find('.thinking-header').trigger('click')
    const nodes = w.findAll('.thinking-step')
    expect(nodes.length).toBe(steps.length)
  })

  it('无步骤时不渲染内容', () => {
    const w = mount(ThinkingPanel, { props: { steps: [], active: false } })
    expect(w.find('.thinking-header').exists()).toBe(false)
  })

  it('流式中动态追加步骤：展开后新步骤全部渲染（v-show 可靠显示）', async () => {
    const w = mount(ThinkingPanel, { props: { steps: [steps[0]], active: true } })
    const body = w.find('.thinking-body')
    // 折叠态：v-show 隐藏（display: none）
    expect((body.element as HTMLElement).style.display).toBe('none')
    await w.find('.thinking-header').trigger('click')
    await nextTick()
    expect((body.element as HTMLElement).style.display).not.toBe('none')
    expect(w.findAll('.thinking-step').length).toBe(1)

    // 模拟流式中 thinking 事件逐步 push
    await w.setProps({ steps: [...steps] })
    expect(w.findAll('.thinking-step').length).toBe(steps.length)
    expect(w.text()).toContain('路由判定')
    expect(w.text()).toContain('web_search')
  })
})
