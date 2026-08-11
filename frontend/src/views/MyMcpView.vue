<script setup lang="ts">
// 我的 MCP：用户维度 MCP 凭据自助管理（spec F7/F10）
// 全局开关（bootstrap 管部署级）+ 用户自己的凭据（生成/启停/吊销）+ 权限配置（限于自己的知识库）
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, CopyDocument, Delete, Connection } from '@element-plus/icons-vue'
import * as mcpApi from '../api/mcp'
import * as configApi from '../api/config'
import * as kbApi from '../api/kb'
import type { Kb, MyMCPKey } from '../api/types'

const loading = ref(true)
const globalEnabled = ref(false)
const mcpPath = ref('/mcp')
const myKey = ref<MyMCPKey | null>(null)

const creating = ref(false)
const toggling = ref(false)
const savingPerms = ref(false)
const kbOptions = ref<Kb[]>([])

// 创建后一次性展示的明文
const createdSecret = ref('')

const MCP_TOOLS = [
  { name: 'list_knowledge_bases', label: '列出知识库' },
  { name: 'get_knowledge_base', label: '知识库详情' },
  { name: 'retrieve', label: '纯检索' },
  { name: 'ask', label: 'RAG 问答' },
  { name: 'list_documents', label: '列出文档' },
  { name: 'get_task', label: '任务状态' },
]

// 权限表单
const form = ref({
  mcpTools: [] as string[],
  scope: '',
  kbIDs: [] as string[],
})

const endpoint = computed(() => `${window.location.origin}${mcpPath.value || '/mcp'}`)

const mcpServersExample = computed(() =>
  JSON.stringify(
    {
      mcpServers: {
        binrag: {
          url: endpoint.value,
          headers: { Authorization: `Bearer ${createdSecret.value || '<API Key>'}` },
        },
      },
    },
    null,
    2,
  ),
)

async function load() {
  loading.value = true
  try {
    const [status, cfg, kbs] = await Promise.all([
      mcpApi.myMCPStatus(),
      configApi.getConfig(),
      kbApi.listKbs(),
    ])
    globalEnabled.value = status.global_enabled
    mcpPath.value = status.mcp_path || cfg.mutable.mcp?.path || '/mcp'
    myKey.value = status.key
    kbOptions.value = kbs ?? []
    if (myKey.value) {
      form.value = {
        mcpTools: [...myKey.value.mcp_tools],
        scope: myKey.value.mcp_kb_scope ?? '',
        kbIDs: [...myKey.value.mcp_kb_ids],
      }
    }
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    loading.value = false
  }
}

async function createKey() {
  creating.value = true
  try {
    const res = await mcpApi.createMyKey()
    createdSecret.value = res.key
    ElMessage.success('已生成 MCP 凭据')
    await load()
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    creating.value = false
  }
}

async function copySecret() {
  if (!createdSecret.value) return
  try {
    await navigator.clipboard.writeText(createdSecret.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动复制')
  }
}

async function toggle(enabled: boolean) {
  toggling.value = true
  try {
    await mcpApi.toggleMyKey(enabled)
    ElMessage.success(enabled ? '已启用 MCP 凭据' : '已停用 MCP 凭据')
    await load()
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    toggling.value = false
  }
}

async function revoke() {
  try {
    await ElMessageBox.confirm('吊销后该 MCP 凭据立即失效，且不可恢复。确定吊销吗？', '吊销确认', {
      confirmButtonText: '吊销',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await mcpApi.deleteMyKey()
    ElMessage.success('已吊销 MCP 凭据')
    createdSecret.value = ''
    await load()
  } catch (err) {
    ElMessage.error((err as Error).message)
  }
}

async function savePermissions() {
  savingPerms.value = true
  try {
    await mcpApi.updateMyPermissions({
      mcp_tools: form.value.mcpTools,
      mcp_kb_scope: form.value.scope,
      mcp_kb_ids: form.value.kbIDs,
    })
    ElMessage.success('已保存 MCP 权限')
    await load()
  } catch (err) {
    ElMessage.error((err as Error).message)
  } finally {
    savingPerms.value = false
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动复制')
  }
}

onMounted(load)
</script>

<template>
  <div class="my-mcp">
    <h2 class="page-title">我的 MCP</h2>

    <div v-if="loading" class="br-muted">加载中…</div>

    <template v-else>
      <el-alert
        v-if="!globalEnabled"
        type="warning"
        :closable="false"
        title="MCP 服务未开启"
        description="请让管理员在系统配置中启用 MCP Server（bootstrap API Key），开启后你的 MCP 凭据才可被外部调用。"
        class="mb16"
      />

      <div class="section">
        <h3 class="section-title">我的凭据</h3>

        <!-- 无凭据：生成 -->
        <div v-if="!myKey" class="cred-empty">
          <el-icon class="cred-icon"><Connection /></el-icon>
          <p class="br-muted">还没有 MCP 凭据。生成后将获得绑定当前账号的 MCP 访问 Key。</p>
          <el-button type="primary" :icon="Plus" :loading="creating" :disabled="!globalEnabled" @click="createKey">
            生成我的 MCP Key
          </el-button>
        </div>

        <!-- 有凭据：状态 + 操作 -->
        <div v-else class="cred-row">
          <div class="cred-info">
            <div class="cred-status">
              <el-tag :type="myKey.enabled ? 'success' : 'info'" size="small">
                {{ myKey.enabled ? '已启用' : '已停用' }}
              </el-tag>
              <span class="br-muted cred-id">{{ myKey.id }}</span>
            </div>
            <div class="cred-actions">
              <el-switch
                v-model="myKey.enabled"
                inline-prompt
                active-text="启用"
                inactive-text="停用"
                :disabled="!globalEnabled || toggling"
                @change="toggle($event as boolean)"
              />
              <el-button size="small" type="danger" text :icon="Delete" @click="revoke">吊销</el-button>
            </div>
          </div>
          <!-- 创建后一次性明文 -->
          <div v-if="createdSecret" class="cred-secret">
            <el-alert type="warning" :closable="false" show-icon class="mb8">
              凭据明文仅此一次展示，请立即复制保存。
            </el-alert>
            <code class="cred-secret-text">{{ createdSecret }}</code>
            <el-button type="primary" size="small" :icon="CopyDocument" @click="copySecret">复制</el-button>
          </div>
        </div>
      </div>

      <!-- 权限配置 -->
      <div v-if="myKey" class="section">
        <h3 class="section-title">权限配置</h3>
        <el-form label-width="100px" label-position="left">
          <el-form-item label="Tool 权限">
            <el-checkbox-group v-model="form.mcpTools" :disabled="!globalEnabled">
              <el-checkbox v-for="t in MCP_TOOLS" :key="t.name" :value="t.name">
                {{ t.name }}<span class="tool-label">{{ t.label }}</span>
              </el-checkbox>
            </el-checkbox-group>
            <div class="hint">不勾选任何 Tool 表示凭据无任何 MCP 权限</div>
          </el-form-item>
          <el-form-item label="知识库范围">
            <el-radio-group v-model="form.scope" :disabled="!globalEnabled">
              <el-radio value="">无</el-radio>
              <el-radio value="all">全部（我的知识库）</el-radio>
              <el-radio value="allowlist">白名单</el-radio>
            </el-radio-group>
            <div class="hint">知识库范围仅限你自己的知识库；「全部」也只检索你创建的知识库</div>
          </el-form-item>
          <el-form-item v-if="form.scope === 'allowlist'" label="白名单知识库">
            <el-select v-model="form.kbIDs" multiple filterable placeholder="选择你的知识库" class="kb-select">
              <el-option v-for="kb in kbOptions" :key="kb.ID" :label="`${kb.Name} (${kb.ID.slice(0, 8)})`" :value="kb.ID" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="savingPerms" :disabled="!globalEnabled" @click="savePermissions">
              保存权限
            </el-button>
          </el-form-item>
        </el-form>
      </div>

      <!-- 连接信息 -->
      <div class="section">
        <h3 class="section-title">连接信息</h3>
        <div class="conn-row">
          <span class="conn-label">Endpoint</span>
          <code class="conn-value">{{ endpoint }}</code>
          <el-button size="small" text :icon="CopyDocument" @click="copyText(endpoint)">复制</el-button>
        </div>
        <div class="conn-row">
          <span class="conn-label">认证方式</span>
          <code class="conn-value">Authorization: Bearer &lt;你的 MCP Key&gt;</code>
        </div>
        <div class="conn-row conn-tools">
          <span class="conn-label">支持 Tool</span>
          <div class="conn-tool-list">
            <el-tag v-for="t in MCP_TOOLS" :key="t.name" size="small" type="primary">{{ t.name }}</el-tag>
          </div>
        </div>
        <div class="conn-example">
          <div class="conn-example-head">
            <span class="conn-label">客户端配置示例</span>
            <el-button size="small" text :icon="CopyDocument" @click="copyText(mcpServersExample)">复制</el-button>
          </div>
          <pre class="conn-pre">{{ mcpServersExample }}</pre>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.my-mcp {
  padding: 24px 28px;
  max-width: 1080px;
}

.page-title {
  margin: 0 0 20px;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.section {
  margin-bottom: 24px;
  padding: 20px 24px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  box-shadow: var(--br-shadow-sm);
}

.section-title {
  margin: 0 0 16px;
  font-size: 15px;
  font-weight: 600;
}

.mb16 {
  margin-bottom: 16px;
}

.mb8 {
  margin-bottom: 8px;
}

.cred-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 24px 0;
  text-align: center;
}

.cred-icon {
  font-size: 40px;
  color: var(--br-text-secondary, #999);
}

.cred-row {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.cred-info {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.cred-status {
  display: flex;
  align-items: center;
  gap: 10px;
}

.cred-id {
  font-size: 12px;
  word-break: break-all;
}

.cred-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.cred-secret {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.cred-secret-text {
  flex: 1;
  padding: 10px 12px;
  background: var(--br-bg-inset);
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-sm, 6px);
  font-size: 13px;
  word-break: break-all;
  user-select: all;
}

.tool-label {
  margin-left: 4px;
  font-size: 12px;
  color: var(--br-text-secondary, #999);
}

.hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--br-text-secondary, #999);
}

.kb-select {
  width: 100%;
}

.conn-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.conn-label {
  width: 120px;
  flex-shrink: 0;
  font-size: 13px;
  color: var(--br-text-secondary, #666);
}

.conn-value {
  padding: 4px 8px;
  background: var(--br-bg-inset);
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-sm, 6px);
  font-size: 12px;
  word-break: break-all;
}

.conn-tools {
  align-items: flex-start;
}

.conn-tool-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.conn-example {
  margin-top: 8px;
}

.conn-example-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.conn-pre {
  margin: 0;
  padding: 12px 14px;
  background: var(--br-bg-inset);
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-sm, 6px);
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
  user-select: all;
}
</style>
