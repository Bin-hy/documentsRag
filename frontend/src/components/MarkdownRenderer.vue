<script setup lang="ts">
// Markdown 渲染 + 代码高亮（marked + highlight.js 按需注册）+ XSS 消毒（DOMPurify）
import { computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import xml from 'highlight.js/lib/languages/xml'
import sql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/github-dark.css'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('sql', sql)

const props = defineProps<{ content: string }>()

const html = computed(() =>
  // DOMPurify 消毒：防止恶意文档注入 <script>/事件属性等 XSS 载荷
  DOMPurify.sanitize(
    marked.parse(props.content, {
      async: false,
      breaks: true,
      gfm: true,
    }) as string,
  ),
)
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -->
  <div class="markdown-body" v-html="html" />
</template>

<style scoped>
.markdown-body {
  line-height: 1.7;
  font-size: 14px;
  word-break: break-word;
}

.markdown-body :deep(p) {
  margin: 0 0 8px;
}

.markdown-body :deep(p:last-child) {
  margin-bottom: 0;
}

.markdown-body :deep(pre) {
  padding: 14px 16px;
  border-radius: 12px;
  overflow-x: auto;
  background: #1b1f2a;
  color: #d6dae4;
  border: 1px solid rgba(255, 255, 255, 0.06);
  box-shadow: inset 0 1px 1px rgba(255, 255, 255, 0.04);
}

.markdown-body :deep(code:not(pre code)) {
  padding: 2px 5px;
  border-radius: 4px;
  background: var(--br-bg-inset);
  font-size: 13px;
}

.markdown-body :deep(pre code) {
  background: transparent;
  padding: 0;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  padding-left: 20px;
  margin: 0 0 8px;
}

.markdown-body :deep(blockquote) {
  margin: 0 0 8px;
  padding: 6px 14px;
  border-left: 3px solid var(--br-primary);
  background: var(--br-primary-soft);
  border-radius: 0 var(--br-radius-md) var(--br-radius-md) 0;
}

.markdown-body :deep(table) {
  border-collapse: collapse;
  margin: 8px 0;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid var(--br-border);
  padding: 6px 10px;
}

.markdown-body :deep(img) {
  max-width: 100%;
}

.markdown-body :deep(a) {
  color: var(--br-primary);
}
</style>
