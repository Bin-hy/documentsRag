<script setup lang="ts">
// 任务列表：文档入库状态、失败原因、重试、删除
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { RefreshRight, Delete } from '@element-plus/icons-vue'
import { useDocStore } from '../stores/doc'
import type { Document, Task } from '../api/types'

const props = defineProps<{ documents: Document[] }>()

const docStore = useDocStore()
const errorDialogVisible = ref(false)
const errorDetail = ref<Task | null>(null)

const statusMap: Record<Document['Status'], { label: string; type: 'info' | 'primary' | 'success' | 'danger' }> = {
  pending: { label: '排队中', type: 'info' },
  processing: { label: '解析中', type: 'primary' },
  completed: { label: '已完成', type: 'success' },
  failed: { label: '失败', type: 'danger' },
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

async function showError(doc: Document) {
  const task = await docStore.fetchTask(doc.TaskID)
  if (!task) {
    ElMessage.warning('未找到任务记录')
    return
  }
  errorDetail.value = task
  errorDialogVisible.value = true
}

async function retry(doc: Document) {
  try {
    await docStore.retry(doc.TaskID)
    ElMessage.success('已重新提交任务')
  } catch (err) {
    ElMessage.error((err as Error).message)
  }
}

async function remove(doc: Document) {
  try {
    await ElMessageBox.confirm(`确定删除文档「${doc.Filename}」吗？`, '删除确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await docStore.remove(doc.ID)
    ElMessage.success('文档已删除')
  } catch (err) {
    ElMessage.error((err as Error).message)
  }
}
</script>

<template>
  <el-table :data="documents" stripe empty-text="暂无文档">
    <el-table-column prop="Filename" label="文件名" min-width="200" show-overflow-tooltip />
    <el-table-column prop="Format" label="格式" width="80" />
    <el-table-column label="大小" width="90">
      <template #default="{ row }">{{ formatSize(row.Size) }}</template>
    </el-table-column>
    <el-table-column label="状态" width="110">
      <template #default="{ row }">
        <el-tag :type="statusMap[row.Status as Document['Status']].type" size="small">
          {{ statusMap[row.Status as Document['Status']].label }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column label="创建时间" width="150">
      <template #default="{ row }">
        {{ new Date(row.CreatedAt).toLocaleString() }}
      </template>
    </el-table-column>
    <el-table-column label="操作" width="200" fixed="right">
      <template #default="{ row }">
        <template v-if="row.Status === 'failed'">
          <el-button size="small" text type="danger" @click="showError(row)">查看原因</el-button>
          <el-button size="small" text type="primary" :icon="RefreshRight" @click="retry(row)">
            重试
          </el-button>
        </template>
        <el-button size="small" text type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
      </template>
    </el-table-column>
  </el-table>

  <el-dialog v-model="errorDialogVisible" title="任务失败原因" width="480px">
    <template v-if="errorDetail">
      <p><strong>文档：</strong>{{ errorDetail.DocumentID }}</p>
      <p><strong>重试次数：</strong>{{ errorDetail.RetryCount }}</p>
      <p><strong>失败原因：</strong></p>
      <pre class="error-text">{{ errorDetail.ErrorMessage || '未知错误' }}</pre>
    </template>
  </el-dialog>
</template>

<style scoped>
.error-text {
  margin: 0;
  padding: 12px;
  max-height: 240px;
  overflow: auto;
  background: var(--br-hover);
  border-radius: 6px;
  font-size: 13px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
