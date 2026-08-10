<script setup lang="ts">
// 拖拽/点击上传面板：多文件、格式过滤
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import type { UploadFile } from 'element-plus'
import { useDocStore } from '../stores/doc'

// 后端支持的解析格式
const ALLOWED_EXTENSIONS = ['.txt', '.md', '.pdf', '.docx', '.csv', '.xlsx', '.html']

const props = defineProps<{ kbId: string }>()

const docStore = useDocStore()
const dragging = ref(false)

function isValidFile(file: File): boolean {
  const name = file.name.toLowerCase()
  return ALLOWED_EXTENSIONS.some((ext) => name.endsWith(ext))
}

async function handleFiles(files: File[]) {
  const valid = files.filter(isValidFile)
  const rejected = files.length - valid.length
  if (rejected > 0) {
    ElMessage.warning(`已跳过 ${rejected} 个不支持的文件（支持：${ALLOWED_EXTENSIONS.join(' ')}）`)
  }
  if (valid.length === 0) return
  try {
    await docStore.upload(valid)
    ElMessage.success(`已上传 ${valid.length} 个文件，开始解析入库`)
  } catch (err) {
    ElMessage.error(`上传失败：${(err as Error).message}`)
  }
}

function onDrop(e: DragEvent) {
  dragging.value = false
  const files = Array.from(e.dataTransfer?.files ?? [])
  if (files.length) handleFiles(files)
}
</script>

<template>
  <div
    class="upload-panel"
    :class="{ dragging }"
    @dragover.prevent="dragging = true"
    @dragleave.prevent="dragging = false"
    @drop.prevent="onDrop"
  >
    <div class="upload-badge">
      <el-icon :size="30"><UploadFilled /></el-icon>
    </div>
    <p class="upload-text">拖拽文件到此处，或</p>
    <el-upload
      :show-file-list="false"
      :multiple="true"
      :auto-upload="false"
      :disabled="docStore.uploading"
      :on-change="(file: UploadFile) => handleFiles([file.raw as File])"
      accept=".txt,.md,.pdf,.docx,.csv,.xlsx,.html"
    >
      <el-button type="primary" plain :loading="docStore.uploading">选择文件上传</el-button>
    </el-upload>
    <p class="br-muted upload-tip">支持 TXT / Markdown / PDF / DOCX / CSV / Excel / HTML，可多选</p>
  </div>
</template>

<style scoped>
.upload-panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 34px 16px;
  border: 1.5px dashed var(--br-border-strong);
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  box-shadow: var(--br-shadow-sm);
  transition:
    border-color var(--br-transition-base),
    background-color var(--br-transition-base),
    box-shadow var(--br-transition-base);
}

.upload-panel.dragging {
  border-color: var(--br-primary);
  background: var(--br-primary-soft);
  box-shadow: 0 0 0 4px var(--br-primary-soft);
}

.upload-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 62px;
  height: 62px;
  border-radius: 18px;
  background: var(--br-primary-soft);
  color: var(--br-primary);
  margin-bottom: 4px;
  transition: transform var(--br-transition-base);
}

.upload-panel.dragging .upload-badge {
  transform: scale(1.06);
}

.upload-text {
  margin: 12px 0;
  color: var(--br-text);
  font-size: 14px;
}

.upload-tip {
  margin: 8px 0 0;
  font-size: 12px;
}
</style>
