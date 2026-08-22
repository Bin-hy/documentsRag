<script setup lang="ts">
// Markdown 阅读器：marked 渲染原文，按 anchor 定位并滚动
import { nextTick, onMounted, ref } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import client from '../../api/client'
import { rawDocumentUrl } from '../../api/doc'
import type { ViewerProps } from './types'

const props = defineProps<ViewerProps>()

const containerRef = ref<HTMLDivElement | null>(null)
const html = ref('')
const error = ref('')

// 与后端 chunker.slugifyHeading 同一规则
function slugifyHeading(s: string): string {
  return s.trim().replace(/[#*_`[\]()>]/g, '').replace(/\s+/g, '-')
}

onMounted(async () => {
  try {
    const resp = await client.get(rawDocumentUrl(props.documentId), { responseType: 'text' })
    const text = resp.data as string
    // DOMPurify 消毒：防止恶意文档注入 <script>/事件属性等 XSS 载荷（与 MarkdownRenderer 一致）
    html.value = DOMPurify.sanitize((await marked.parse(text)) as string)

    await nextTick()
    // 渲染后为各标题生成锚点 id（避免依赖 marked renderer 签名差异）
    containerRef.value?.querySelectorAll('h1,h2,h3,h4,h5,h6').forEach((h) => {
      if (!h.id) h.id = slugifyHeading(h.textContent ?? '')
    })

    const anchor = props.location.anchor || (props.location.heading ? slugifyHeading(props.location.heading) : '')
    if (anchor) {
      document.getElementById(anchor)?.scrollIntoView({ block: 'start' })
    }
  } catch (e) {
    error.value = (e as Error).message
  }
})
</script>

<template>
  <div class="markdown-viewer">
    <div v-if="error" class="viewer-error">{{ error }}</div>
    <!-- eslint-disable-next-line vue/no-v-html -->
    <div v-else ref="containerRef" class="markdown-body" v-html="html" />
  </div>
</template>

<style scoped>
.markdown-viewer {
  max-height: 74vh;
  overflow: auto;
}
.markdown-body {
  font-size: 14px;
  line-height: 1.75;
  color: var(--br-text);
}
.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3) {
  margin: 1em 0 0.5em;
  scroll-margin-top: 12px;
}
.markdown-body :deep(pre) {
  background: var(--br-bg-inset, #f6f6f7);
  padding: 10px 12px;
  border-radius: 8px;
  overflow: auto;
}
.markdown-body :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
}
.markdown-body :deep(table) {
  border-collapse: collapse;
}
.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid var(--br-border, #e5e5e7);
  padding: 4px 10px;
}
.viewer-error {
  padding: 16px;
  color: #d33;
  font-size: 13px;
}
</style>
