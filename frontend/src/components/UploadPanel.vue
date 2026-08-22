<script setup lang="ts">
// 拖拽/点击上传面板：多文件、动态格式过滤（以后端 supported-types 为准）
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import type { UploadFile } from 'element-plus'
import { useDocStore } from '../stores/doc'
import { getSupportedTypes } from '../api/doc'
import type { SupportedType } from '../api/types'

// 后端不可用时的兜底（与旧行为一致，避免上传面板失效）
const FALLBACK_EXTS = ['.txt', '.md', '.pdf', '.docx', '.csv', '.xlsx', '.html']

const props = defineProps<{ kbId: string }>()

const docStore = useDocStore()
const dragging = ref(false)

// 初始用兜底文本扩展名，加载到后端列表后覆盖（避免加载窗口期所有文件都被判不支持）
const supportedTypes = ref<SupportedType[]>(
  FALLBACK_EXTS.map((ext) => ({ ext, category: 'text' as const, supported: true })),
)

const supportedExts = computed(() =>
  supportedTypes.value.filter((t) => t.supported).map((t) => t.ext),
)
const accept = computed(() => supportedExts.value.join(','))

const categoryLabel: Record<SupportedType['category'], string> = {
  text: '文本',
  image: '图片',
  audio: '音频',
  video: '视频',
}

function shortReason(t: SupportedType): string {
  const r = t.reason ?? ''
  if (r.includes('vision_embedding')) return '场景检测视觉 embedding 未配置'
  if (r.includes('speech')) return '语音转写未配置'
  if (r.includes('vision')) return '视觉能力未配置'
  return r
}

// 已认识但当前能力未配置的类型分组提示
const unsupportedHints = computed(() => {
  const groups = new Map<SupportedType['category'], SupportedType[]>()
  for (const t of supportedTypes.value) {
    if (t.supported) continue
    if (!groups.has(t.category)) groups.set(t.category, [])
    groups.get(t.category)!.push(t)
  }
  return [...groups.entries()].map(([category, items]) => {
    const exts = items.map((i) => i.ext).join(' ')
    return `${categoryLabel[category]}（${exts}）：${shortReason(items[0])}`
  })
})

onMounted(async () => {
  try {
    supportedTypes.value = await getSupportedTypes()
  } catch {
    // 拉取失败保持兜底文本扩展名，不做额外提示
  }
})

function isValidFile(file: File): boolean {
  const name = file.name.toLowerCase()
  return supportedExts.value.some((ext) => name.endsWith(ext))
}

async function handleFiles(files: File[]) {
  const valid = files.filter(isValidFile)
  const rejected = files.length - valid.length
  if (rejected > 0) {
    ElMessage.warning(`已跳过 ${rejected} 个不支持的文件（支持：${supportedExts.value.join(' ')}）`)
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
      :accept="accept"
    >
      <el-button type="primary" plain :loading="docStore.uploading">选择文件上传</el-button>
    </el-upload>
    <p class="br-muted upload-tip">
      <template v-if="supportedExts.length">支持：{{ supportedExts.join(' ') }}，可多选</template>
      <template v-if="unsupportedHints.length">
        <br /><span class="tip-unsupported">{{ unsupportedHints.join('；') }}</span>
      </template>
    </p>
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

.tip-unsupported {
  color: var(--br-text-muted, #888);
}
</style>
