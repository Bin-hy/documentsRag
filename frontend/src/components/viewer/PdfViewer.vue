<script setup lang="ts">
// PDF 阅读器：pdfjs-dist 带鉴权加载原文，跳转指定页，支持翻页
import { onMounted, ref } from 'vue'
import * as pdfjsLib from 'pdfjs-dist'
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { rawDocumentUrl } from '../../api/doc'
import { getStoredToken, getStoredApiKey } from '../../api/client'
import type { ViewerProps } from './types'

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl

const props = defineProps<ViewerProps>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const pageNum = ref(1)
const numPages = ref(0)
const loading = ref(false)
const error = ref('')

let pdfDoc: pdfjsLib.PDFDocumentProxy | null = null

async function renderPage(num: number) {
  if (!pdfDoc || !canvasRef.value) return
  loading.value = true
  try {
    const page = await pdfDoc.getPage(num)
    const viewport = page.getViewport({ scale: 1.2 })
    const canvas = canvasRef.value
    canvas.width = viewport.width
    canvas.height = viewport.height
    await page.render({ canvas, viewport }).promise
    pageNum.value = num
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  try {
    const token = getStoredToken() || getStoredApiKey()
    const loadingTask = pdfjsLib.getDocument({
      url: rawDocumentUrl(props.documentId),
      httpHeaders: token ? { Authorization: `Bearer ${token}` } : undefined,
    })
    pdfDoc = await loadingTask.promise
    numPages.value = pdfDoc.numPages
    await renderPage(props.location.page ?? 1)
  } catch (e) {
    error.value = (e as Error).message
  }
})
</script>

<template>
  <div class="pdf-viewer">
    <div v-if="error" class="viewer-error">{{ error }}</div>
    <div v-else class="pdf-body" v-loading="loading">
      <canvas ref="canvasRef" />
    </div>
    <div v-if="!error && numPages > 1" class="pdf-toolbar">
      <el-button size="small" :disabled="pageNum <= 1" @click="renderPage(pageNum - 1)">上一页</el-button>
      <span class="pdf-page-info">{{ pageNum }} / {{ numPages }}</span>
      <el-button size="small" :disabled="pageNum >= numPages" @click="renderPage(pageNum + 1)">下一页</el-button>
    </div>
  </div>
</template>

<style scoped>
.pdf-viewer {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.pdf-body {
  max-height: 74vh;
  overflow: auto;
  text-align: center;
}
.pdf-toolbar {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
}
.pdf-page-info {
  font-size: 12px;
  color: var(--br-text-secondary, #888);
}
.viewer-error {
  padding: 16px;
  color: #d33;
  font-size: 13px;
}
</style>
