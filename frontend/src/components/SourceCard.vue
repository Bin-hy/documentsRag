<script setup lang="ts">
// 引用来源卡片：文件名 + 标题 + 分数
import { Document } from '@element-plus/icons-vue'
import type { ChatSource } from '../api/types'

defineProps<{ sources: ChatSource[] }>()
</script>

<template>
  <div v-if="sources && sources.length" class="source-list">
    <div class="source-title">引用来源</div>
    <div class="source-cards">
      <div v-for="src in sources" :key="src.id" class="source-card">
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
</style>
