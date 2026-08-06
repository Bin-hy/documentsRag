<script setup lang="ts">
// 引用来源卡片：文件名 + 标题 + 分数；点击查看 chunk 原文
import { ref } from 'vue'
import { Document } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import type { ChatSource } from '../api/types'
import { getChunk, type ChunkDetail } from '../api/chunk'

const props = defineProps<{ sources: ChatSource[] }>()

const dialogVisible = ref(false)
const loading = ref(false)
const detail = ref<ChunkDetail | null>(null)

async function openChunk(src: ChatSource) {
  loading.value = true
  detail.value = null
  dialogVisible.value = true
  try {
    detail.value = await getChunk(src.id)
  } catch (e) {
    dialogVisible.value = false
    ElMessage.error('加载引用原文失败：' + (e instanceof Error ? e.message : String(e)))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div v-if="sources && sources.length" class="source-list">
    <div class="source-title">引用来源（点击查看原文）</div>
    <div class="source-cards">
      <div
        v-for="src in sources"
        :key="src.id"
        class="source-card"
        @click="openChunk(src)"
      >
        <el-icon class="source-icon"><Document /></el-icon>
        <div class="source-info">
          <div class="source-filename br-text-ellipsis" :title="src.filename">{{ src.filename }}</div>
          <div v-if="src.heading" class="source-heading br-text-ellipsis" :title="src.heading">
            {{ src.heading }}
          </div>
        </div>
        <el-tag size="small" type="info" class="source-score">{{ src.score.toFixed(2) }}</el-tag>
      </div>
    </div>

    <!-- chunk 原文弹窗 -->
    <el-dialog v-model="dialogVisible" :title="detail?.filename || '引用原文'" width="680px" top="8vh">
      <div v-loading="loading" class="chunk-content">
        <pre v-if="detail" class="chunk-pre">{{ detail.content }}</pre>
      </div>
    </el-dialog>
  </div>
</template>

<style scoped>
.source-list {
  margin-top: 10px;
}

.source-title {
  font-size: 12px;
  color: var(--br-text-secondary);
  margin-bottom: 6px;
}

.source-cards {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.source-card {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 260px;
  padding: 8px 10px;
  border: 1px solid var(--br-border);
  border-radius: 8px;
  background: var(--br-bg);
  cursor: pointer;
  transition: border-color 0.15s;
}

.source-card:hover {
  border-color: var(--br-primary);
}

.source-icon {
  color: var(--br-primary);
  flex-shrink: 0;
}

.source-info {
  min-width: 0;
  flex: 1;
}

.source-filename {
  font-size: 13px;
}

.source-heading {
  font-size: 12px;
}

.source-score {
  flex-shrink: 0;
}

.chunk-content {
  max-height: 60vh;
  overflow: auto;
}

.chunk-pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.7;
  color: var(--br-text);
}
</style>
