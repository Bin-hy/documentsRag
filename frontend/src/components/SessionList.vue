<script setup lang="ts">
// 会话侧栏：新建 / 切换 / 删除
import { Plus, Delete } from '@element-plus/icons-vue'
import type { SessionMeta } from '../api/types'

defineProps<{
  sessions: SessionMeta[]
  activeId: string
}>()

const emit = defineEmits<{
  (e: 'new'): void
  (e: 'switch', id: string): void
  (e: 'delete', id: string): void
}>()
</script>

<template>
  <div class="session-panel">
    <el-button class="new-session-btn" type="primary" :icon="Plus" @click="emit('new')">
      新建会话
    </el-button>

    <div class="session-list">
      <div
        v-for="s in sessions"
        :key="s.id"
        class="session-item"
        :class="{ active: s.id === activeId }"
        @click="emit('switch', s.id)"
      >
        <span class="session-title br-text-ellipsis" :title="s.title">{{ s.title }}</span>
        <el-icon class="session-delete" @click.stop="emit('delete', s.id)"><Delete /></el-icon>
      </div>
      <div v-if="sessions.length === 0" class="br-muted session-empty">暂无会话</div>
    </div>
  </div>
</template>

<style scoped>
.session-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 12px;
}

.new-session-btn {
  width: 100%;
  margin-bottom: 12px;
}

.session-list {
  flex: 1;
  overflow-y: auto;
}

.session-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 10px;
  margin-bottom: 4px;
  border-radius: 8px;
  cursor: pointer;
  font-size: 13px;
  color: var(--br-text);
}

.session-item:hover {
  background: var(--br-hover);
}

.session-item.active {
  background: var(--br-primary);
  color: #fff;
}

.session-title {
  flex: 1;
}

.session-delete {
  opacity: 0;
  transition: opacity 0.15s;
  flex-shrink: 0;
}

.session-item:hover .session-delete {
  opacity: 0.7;
}

.session-delete:hover {
  opacity: 1 !important;
}

.session-empty {
  text-align: center;
  padding: 24px 0;
  font-size: 13px;
}
</style>
