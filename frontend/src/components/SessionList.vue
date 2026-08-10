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
    <button class="new-session-btn" @click="emit('new')">
      <el-icon class="ns-icon"><Plus /></el-icon>
      <span>新建会话</span>
    </button>

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
      <div v-if="sessions.length === 0" class="br-muted session-empty">
        暂无会话，点击上方新建
      </div>
    </div>
  </div>
</template>

<style scoped>
.session-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 14px 12px 12px;
}

/* 新建会话：胶囊渐变按钮 */
.new-session-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  width: 100%;
  margin-bottom: 14px;
  padding: 10px 14px;
  border: none;
  border-radius: var(--br-radius-pill);
  background: linear-gradient(135deg, #5f6be0, #404ab0);
  color: #fff;
  font-size: 13.5px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: 0 6px 14px rgba(79, 91, 213, 0.3);
  transition:
    transform var(--br-transition-fast),
    box-shadow var(--br-transition-fast);
}

.new-session-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 20px rgba(79, 91, 213, 0.36);
}

.new-session-btn:active {
  transform: scale(0.98);
}

.ns-icon {
  font-size: 15px;
}

.session-list {
  flex: 1;
  overflow-y: auto;
}

.session-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 12px;
  margin-bottom: 3px;
  border-radius: var(--br-radius-md);
  cursor: pointer;
  font-size: 13px;
  color: var(--br-text);
  transition:
    background-color var(--br-transition-fast),
    color var(--br-transition-fast);
}

.session-item:hover {
  background: var(--br-bg-hover);
}

.session-item.active {
  background: var(--br-primary-soft);
  color: var(--br-primary);
  font-weight: 600;
}

.session-title {
  flex: 1;
}

.session-delete {
  opacity: 0;
  transition: opacity var(--br-transition-fast);
  flex-shrink: 0;
  border-radius: 6px;
  padding: 2px;
}

.session-item:hover .session-delete {
  opacity: 0.7;
}

.session-delete:hover {
  opacity: 1 !important;
  color: #f56c6c;
}

.session-empty {
  text-align: center;
  padding: 26px 0;
  font-size: 12.5px;
}
</style>
