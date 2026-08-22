// Viewer 注册表：类型 → 组件，可插拔（新增阅读器类型只需 register，不改核心）
// 内置 Viewer 在模块加载时注册，保证 resolveViewer 在任何时刻可用。
import type { Component } from 'vue'
import type { ViewerType } from './types'
import PdfViewer from './PdfViewer.vue'
import VideoViewer from './VideoViewer.vue'
import AudioViewer from './AudioViewer.vue'
import MarkdownViewer from './MarkdownViewer.vue'

const registry = new Map<ViewerType, Component>([
  ['pdf', PdfViewer],
  ['video', VideoViewer],
  ['audio', AudioViewer],
  ['markdown', MarkdownViewer],
])

export function registerViewer(type: ViewerType, comp: Component): void {
  registry.set(type, comp)
}

export function resolveViewer(type: ViewerType): Component | undefined {
  return registry.get(type)
}
