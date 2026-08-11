<script setup lang="ts">
// API Key 管理：创建（明文仅一次）、启停、删除、MCP 权限配置
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, CopyDocument, Delete, Key } from '@element-plus/icons-vue'
import * as keyApi from '../api/key'
import * as configApi from '../api/config'
import * as kbApi from '../api/kb'
import type { ApiKeyView, CreateKeyResult, Kb } from '../api/types'

const keys = ref<ApiKeyView[]>([])
const loading = ref(false)
// MCP 权限编辑可用性：仅 bootstrap Key（后端 GET /config 权威判断，spec F3）
const canEditMCP = ref(false)

const createDialogVisible = ref(false)
const createName = ref('')
const creating = ref(false)

const resultDialogVisible = ref(false)
const createdKey = ref<CreateKeyResult | null>(null)

// MCP 权限编辑对话框状态（spec F2）
const mcpDialogVisible = ref(false)
const editingKey = ref<ApiKeyView | null>(null)
const mcpTools = ref<string[]>([])
const mcpScope = ref('')
const mcpKbIDs = ref<string[]>([])
const kbOptions = ref<Kb[]>([])
const savingMCP = ref(false)

// 6 个 MCP Tool（与后端注册一致）
const MCP_TOOLS = [
  { name: 'list_knowledge_bases', label: '列出知识库' },
  { name: 'get_knowledge_base', label: '知识库详情' },
  { name: 'retrieve', label: '纯检索' },
  { name: 'ask', label: 'RAG 问答' },
  { name: 'list_documents', label: '列出文档' },
  { name: 'get_task', label: '任务状态' },
]

async function load() {
  loading.value = true
  try {
    const [list, cfg] = await Promise.all([keyApi.listKeys(), configApi.getConfig()])
    keys.value = list ?? []
    canEditMCP.value = !!cfg?.is_bootstrap
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    loading.value = false
  }
}

// MCP 权限展示（spec F1：空权限一律「未配置」，不出现 null）
function scopeLabel(k: ApiKeyView): string {
  if (k.mcp_kb_scope === 'all') return '全部'
  if (k.mcp_kb_scope === 'allowlist') return `白名单(${k.mcp_kb_ids?.length ?? 0})`
  return ''
}

function isUnconfigured(k: ApiKeyView): boolean {
  return (k.mcp_tools?.length ?? 0) === 0 && (k.mcp_kb_scope ?? '') === ''
}

// 打开权限编辑对话框（spec F2）
function openMCPDialog(k: ApiKeyView) {
  editingKey.value = k
  mcpTools.value = [...(k.mcp_tools ?? [])]
  mcpScope.value = k.mcp_kb_scope ?? ''
  mcpKbIDs.value = [...(k.mcp_kb_ids ?? [])]
  if (kbOptions.value.length === 0) {
    kbApi.listKbs().then((kbs) => (kbOptions.value = kbs ?? [])).catch(() => (kbOptions.value = []))
  }
  mcpDialogVisible.value = true
}

async function saveMCP() {
  if (!editingKey.value) return
  savingMCP.value = true
  try {
    await keyApi.updateKeyPermissions(editingKey.value.id, {
      mcp_tools: mcpTools.value,
      mcp_kb_scope: mcpScope.value,
      mcp_kb_ids: mcpKbIDs.value,
    })
    ElMessage.success('已更新 MCP 权限')
    mcpDialogVisible.value = false
    await load()
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    savingMCP.value = false
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
      <el-table-column label="MCP 权限" min-width="240">
        <template #default="{ row }">
          <div v-if="isUnconfigured(row)" class="mcp-cell">
            <el-tag size="small" type="info">未配置</el-tag>
          </div>
          <div v-else class="mcp-cell">
            <el-tag v-for="t in row.mcp_tools" :key="t" size="small" type="primary" class="mcp-tag">{{ t }}</el-tag>
            <el-tag size="small" :type="row.mcp_kb_scope === 'all' ? 'success' : 'warning'" class="mcp-tag">
              {{ scopeLabel(row) }}
            </el-tag>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="170" fixed="right">
        <template #default="{ row }">
          <el-tooltip :content="canEditMCP ? '' : '需要 bootstrap API Key 才能编辑 MCP 权限'" :disabled="canEditMCP">
            <el-button
              size="small"
              text
              :icon="Key"
              :disabled="!canEditMCP"
              @click="openMCPDialog(row)"
            >MCP 权限</el-button>
          </el-tooltip>
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

    <!-- MCP 权限编辑对话框（spec F2） -->
    <el-dialog v-model="mcpDialogVisible" title="编辑 MCP 权限" width="560px" :close-on-click-modal="false">
      <template v-if="editingKey">
        <div class="mcp-dialog-key">API Key：{{ editingKey.name }}（{{ editingKey.id }}）</div>
        <el-form label-width="90px">
          <el-form-item label="Tool 权限">
            <el-checkbox-group v-model="mcpTools">
              <el-checkbox v-for="t in MCP_TOOLS" :key="t.name" :value="t.name">
                {{ t.name }}<span class="mcp-tool-label">{{ t.label }}</span>
              </el-checkbox>
            </el-checkbox-group>
            <div class="mcp-hint">不勾选任何 Tool 表示该 Key 无任何 MCP Tool 权限</div>
          </el-form-item>
          <el-form-item label="知识库范围">
            <el-radio-group v-model="mcpScope">
              <el-radio value="">无</el-radio>
              <el-radio value="all">全部</el-radio>
              <el-radio value="allowlist">白名单</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="mcpScope === 'allowlist'" label="白名单知识库">
            <el-select v-model="mcpKbIDs" multiple filterable placeholder="选择可访问的知识库" class="mcp-kb-select">
              <el-option v-for="kb in kbOptions" :key="kb.ID" :label="`${kb.Name} (${kb.ID.slice(0, 8)})`" :value="kb.ID" />
            </el-select>
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="mcpDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="savingMCP" @click="saveMCP">保存</el-button>
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
  padding: 24px 28px;
}

.mcp-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.mcp-tag {
  margin-right: 0;
}

.mcp-dialog-key {
  margin-bottom: 12px;
  font-size: 13px;
  color: var(--br-text-secondary, #666);
  word-break: break-all;
}

.mcp-tool-label {
  margin-left: 4px;
  font-size: 12px;
  color: var(--br-text-secondary, #999);
}

.mcp-hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--br-text-secondary, #999);
}

.mcp-kb-select {
  width: 100%;
}

.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
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
  padding: 12px 14px;
  background: var(--br-bg-inset);
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-md);
  font-size: 13px;
  word-break: break-all;
  user-select: all;
}
</style>
