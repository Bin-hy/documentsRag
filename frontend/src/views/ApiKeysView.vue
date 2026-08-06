<script setup lang="ts">
// API Key 管理：创建（明文仅一次）、启停、删除
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, CopyDocument, Delete } from '@element-plus/icons-vue'
import * as keyApi from '../api/key'
import type { ApiKeyView, CreateKeyResult } from '../api/types'

const keys = ref<ApiKeyView[]>([])
const loading = ref(false)

const createDialogVisible = ref(false)
const createName = ref('')
const creating = ref(false)

const resultDialogVisible = ref(false)
const createdKey = ref<CreateKeyResult | null>(null)

async function load() {
  loading.value = true
  try {
    keys.value = (await keyApi.listKeys()) ?? []
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    loading.value = false
  }
}

async function submitCreate() {
  if (!createName.value.trim()) {
    ElMessage.warning('请输入 Key 名称')
    return
  }
  creating.value = true
  try {
    createdKey.value = await keyApi.createKey(createName.value.trim())
    createDialogVisible.value = false
    createName.value = ''
    resultDialogVisible.value = true
    await load()
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    creating.value = false
  }
}

async function copyKey() {
  if (!createdKey.value) return
  try {
    await navigator.clipboard.writeText(createdKey.value.key)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

async function toggle(k: ApiKeyView) {
  try {
    await keyApi.toggleKey(k.id, k.enabled)
    ElMessage.success(k.enabled ? '已启用' : '已停用')
  } catch (err) {
    k.enabled = !k.enabled // 失败回滚开关状态
    ElMessage.error((err as Error).message)
  }
}

async function remove(k: ApiKeyView) {
  try {
    await ElMessageBox.confirm(`确定删除 Key「${k.name}」吗？删除后立即失效。`, '删除确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await keyApi.deleteKey(k.id)
    ElMessage.success('已删除')
    await load()
  } catch (err) {
    ElMessage.error((err as Error).message)
  }
}

function formatTime(iso: string | null): string {
  if (!iso) return '从未使用'
  return new Date(iso).toLocaleString()
}

onMounted(load)
</script>

<template>
  <div class="keys-page">
    <div class="page-head">
      <h2 class="page-title">API Key 管理</h2>
      <el-button type="primary" :icon="Plus" @click="createDialogVisible = true">创建 API Key</el-button>
    </div>

    <el-empty v-if="!loading && keys.length === 0" description="还没有 API Key，创建第一个用于登录访问" />

    <el-table :data="keys" stripe v-loading="loading" empty-text="暂无 API Key">
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-switch
            v-model="row.enabled"
            inline-prompt
            active-text="启用"
            inactive-text="停用"
            @change="toggle(row)"
          />
        </template>
      </el-table-column>
      <el-table-column label="最后使用时间" width="180">
        <template #default="{ row }">{{ formatTime(row.last_used_at) }}</template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button size="small" text type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建对话框 -->
    <el-dialog v-model="createDialogVisible" title="创建 API Key" width="400px">
      <el-form @submit.prevent="submitCreate">
        <el-form-item label="名称">
          <el-input v-model="createName" placeholder="例如：本地开发" maxlength="64" @keyup.enter="submitCreate" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="submitCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- 明文一次性展示 -->
    <el-dialog v-model="resultDialogVisible" title="API Key 创建成功" width="520px" :close-on-click-modal="false">
      <el-alert type="warning" :closable="false" show-icon class="key-warn">
        请立即复制保存。关闭此窗口后将无法再次查看明文 Key。
      </el-alert>
      <div class="key-result">
        <code class="key-text">{{ createdKey?.key }}</code>
        <el-button type="primary" :icon="CopyDocument" @click="copyKey">复制</el-button>
      </div>
      <template #footer>
        <el-button type="primary" @click="resultDialogVisible = false">我已保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.keys-page {
  padding: 20px 24px;
}

.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.page-title {
  margin: 0;
  font-size: 20px;
}

.key-warn {
  margin-bottom: 12px;
}

.key-result {
  display: flex;
  align-items: center;
  gap: 12px;
}

.key-text {
  flex: 1;
  padding: 10px 12px;
  background: var(--br-hover);
  border-radius: 6px;
  font-size: 13px;
  word-break: break-all;
  user-select: all;
}
</style>
