<script setup lang="ts">
// 系统配置管理：可修改组（LLM/Retriever/Strategy/Loader/MCP）+ 只读组（需重启生效）
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import { getConfig, updateConfig } from '../api/config'
import type { ConfigView } from '../api/types'

const loading = ref(true)
const saving = ref(false)
const savingMCP = ref(false)
const isBootstrap = ref(false)

// 可修改表单
const form = reactive({
  temperature: 0.7,
  maxTokens: 2048,
  topK: 5,
  vectorWeight: 0.7,
  bm25Weight: 0.3,
  strategy: {
    query: 'multi' as 'single' | 'multi',
    decomposition: 'off' as 'off' | 'parallel' | 'sequential',
    step_back: 'off' as 'off' | 'on',
    hyde: 'off' as 'off' | 'on',
    routing: 'off' as 'off' | 'auto',
  },
  // MCP Server（重启生效，spec F4）
  mcp: {
    enabled: false,
    path: '/mcp',
    auditParamLimit: 2000,
  },
})

const readOnly = ref<ConfigView['read_only']>([])

// 权重联动：调整一个，另一个自动补足（算法要求和=1）
function onVectorWeightChange() {
  form.bm25Weight = Math.round((1 - form.vectorWeight) * 100) / 100
}

function onBm25WeightChange() {
  form.vectorWeight = Math.round((1 - form.bm25Weight) * 100) / 100
}

async function load() {
  loading.value = true
  try {
    const view = await getConfig()
    form.temperature = view.mutable.llm.temperature
    form.maxTokens = view.mutable.llm.max_tokens
    form.topK = view.mutable.retriever.top_k
    form.vectorWeight = view.mutable.retriever.vector_weight
    form.bm25Weight = view.mutable.retriever.bm25_weight
    Object.assign(form.strategy, view.mutable.rag_strategy)
    // MCP 分组（后端运行时值，spec F4）
    form.mcp.enabled = view.mutable.mcp?.enabled ?? false
    form.mcp.path = view.mutable.mcp?.path ?? '/mcp'
    form.mcp.auditParamLimit = view.mutable.mcp?.audit_param_limit ?? 2000
    readOnly.value = view.read_only ?? []
    // bootstrap 状态由后端权威判断（middleware 校验当前 Key），不再前端猜测字符串
    isBootstrap.value = !!view.is_bootstrap
  } catch (e) {
    ElMessage.error('加载配置失败：' + (e instanceof Error ? e.message : String(e)))
  } finally {
    loading.value = false
  }
}

async function save() {
  // 前端兜底：保证和=1（算法要求）
  const total = form.vectorWeight + form.bm25Weight
  if (Math.abs(total - 1) > 0.001) {
    ElMessage.warning(`向量权重 + BM25 权重之和必须为 1，当前 ${total.toFixed(2)}，已自动修正`)
    form.bm25Weight = 1 - form.vectorWeight
  }
  saving.value = true
  try {
    await updateConfig({
      llm: { temperature: form.temperature, max_tokens: form.maxTokens },
      retriever: { top_k: form.topK, vector_weight: form.vectorWeight, bm25_weight: form.bm25Weight },
      rag_strategy: form.strategy,
    })
    ElMessage.success('配置已保存并热重载（仅影响新请求）')
  } catch (e) {
    ElMessage.error('保存失败：' + (e instanceof Error ? e.message : String(e)))
  } finally {
    saving.value = false
  }
}

// MCP Server 保存（spec F4）：MCP 参数重启生效（enabled/path 影响路由挂载）
async function saveMCP() {
  savingMCP.value = true
  try {
    await updateConfig({
      mcp: { enabled: form.mcp.enabled, path: form.mcp.path, audit_param_limit: form.mcp.auditParamLimit },
    })
    ElMessage.success('MCP 配置已保存，重启服务后生效')
  } catch (e) {
    ElMessage.error('保存失败：' + (e instanceof Error ? e.message : String(e)))
  } finally {
    savingMCP.value = false
  }
}

// 连接信息（spec F5）：endpoint 由当前请求 host + 配置 path 拼接
const MCP_TOOLS = ['list_knowledge_bases', 'get_knowledge_base', 'retrieve', 'ask', 'list_documents', 'get_task']
const mcpEndpoint = computed(() => `${window.location.origin}${form.mcp.path || '/mcp'}`)
const mcpServersExample = computed(() =>
  JSON.stringify(
    {
      mcpServers: {
        binrag: {
          url: mcpEndpoint.value,
          headers: { Authorization: 'Bearer <API Key>' },
        },
      },
    },
    null,
    2,
  ),
)

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

onMounted(load)
</script>

<template>
  <div class="settings">
    <h2 class="page-title">系统配置</h2>

    <div v-if="loading" class="br-muted">加载中…</div>

    <template v-else>
      <el-alert
        v-if="!isBootstrap"
        type="warning"
        :closable="false"
        title="当前 API Key 非 bootstrap Key，配置为只读"
        description="修改配置需要使用 bootstrap API Key（config.local.yaml 的 bootstrap_api_key）。"
        class="mb16"
      />

      <div class="section">
        <h3 class="section-title">可修改配置</h3>
        <el-form label-width="140px" label-position="left" class="mb16">
          <el-form-item label="LLM 温度">
            <el-slider v-model="form.temperature" :min="0" :max="2" :step="0.1" show-input :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item label="LLM 最大 Token">
            <el-input-number v-model="form.maxTokens" :min="256" :max="8192" :step="256" :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item label="Retriever Top-K">
            <el-input-number v-model="form.topK" :min="1" :max="50" :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item label="向量权重">
            <div class="weight-row">
              <el-slider
                v-model="form.vectorWeight"
                :min="0"
                :max="1"
                :step="0.05"
                show-input
                :disabled="!isBootstrap"
                @input="onVectorWeightChange"
              />
              <span class="br-muted weight-note">BM25 自动 = {{ (1 - form.vectorWeight).toFixed(2) }}</span>
            </div>
          </el-form-item>
          <el-form-item label="BM25 权重">
            <div class="weight-row">
              <el-slider
                v-model="form.bm25Weight"
                :min="0"
                :max="1"
                :step="0.05"
                show-input
                :disabled="!isBootstrap"
                @input="onBm25WeightChange"
              />
              <span class="br-muted weight-note">向量自动 = {{ (1 - form.bm25Weight).toFixed(2) }}</span>
            </div>
          </el-form-item>
          <el-form-item>
            <el-alert
              type="info"
              :closable="false"
              title="算法要求：向量权重 + BM25 权重之和必须为 1"
              description="调整任意一个权重，另一个会自动补足，保证混合检索（RRF 融合）比例正确。"
            />
          </el-form-item>
          <el-form-item label="查询模式">
            <el-radio-group v-model="form.strategy.query" :disabled="!isBootstrap">
              <el-radio value="multi">多查询</el-radio>
              <el-radio value="single">单查询</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="问题分解">
            <el-radio-group v-model="form.strategy.decomposition" :disabled="!isBootstrap">
              <el-radio value="off">关闭</el-radio>
              <el-radio value="parallel">并行</el-radio>
              <el-radio value="sequential">顺序</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="回退查询">
            <el-switch v-model="form.strategy.step_back" active-value="on" inactive-value="off" :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item label="HyDE">
            <el-switch v-model="form.strategy.hyde" active-value="on" inactive-value="off" :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item label="自动路由">
            <el-switch v-model="form.strategy.routing" active-value="auto" inactive-value="off" :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="saving" :disabled="!isBootstrap" @click="save">保存并热重载</el-button>
          </el-form-item>
        </el-form>
      </div>

      <!-- MCP Server 卡片（spec F4/F5）：参数编辑（重启生效）+ 连接信息 -->
      <div class="section">
        <h3 class="section-title">MCP Server</h3>
        <el-form label-width="140px" label-position="left" class="mb16">
          <el-form-item label="启用 MCP Server">
            <el-switch v-model="form.mcp.enabled" active-text="启用" inactive-text="停用" :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item label="端点路径">
            <el-input v-model="form.mcp.path" placeholder="/mcp" :disabled="!isBootstrap" class="mcp-path-input" />
          </el-form-item>
          <el-form-item label="审计截断长度">
            <el-input-number v-model="form.mcp.auditParamLimit" :min="0" :step="100" :disabled="!isBootstrap" />
          </el-form-item>
          <el-form-item>
            <el-alert
              type="info"
              :closable="false"
              title="MCP 参数修改后需重启服务生效"
              description="enabled 与 path 在服务启动时挂载路由；audit_param_limit 在启动时创建审计队列。保存仅持久化配置。"
            />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="savingMCP" :disabled="!isBootstrap" @click="saveMCP">保存 MCP 配置</el-button>
          </el-form-item>
        </el-form>

        <div class="mcp-conn">
          <h4 class="mcp-conn-title">连接信息</h4>
          <div class="mcp-conn-row">
            <span class="mcp-conn-label">Endpoint</span>
            <code class="mcp-conn-value">{{ mcpEndpoint }}</code>
            <el-button size="small" text :icon="CopyDocument" @click="copyText(mcpEndpoint)">复制</el-button>
          </div>
          <div class="mcp-conn-row">
            <span class="mcp-conn-label">认证方式</span>
            <code class="mcp-conn-value">Authorization: Bearer &lt;API Key&gt;</code>
          </div>
          <div class="mcp-conn-row mcp-conn-tools">
            <span class="mcp-conn-label">支持 Tool</span>
            <div class="mcp-conn-tool-list">
              <el-tag v-for="t in MCP_TOOLS" :key="t" size="small" type="primary">{{ t }}</el-tag>
            </div>
          </div>
          <div class="mcp-conn-example">
            <div class="mcp-conn-example-head">
              <span class="mcp-conn-label">客户端配置示例</span>
              <el-button size="small" text :icon="CopyDocument" @click="copyText(mcpServersExample)">复制</el-button>
            </div>
            <pre class="mcp-conn-pre">{{ mcpServersExample }}</pre>
          </div>
        </div>
      </div>

      <div class="section">
        <h3 class="section-title">启动级配置（需重启生效）</h3>
        <el-table :data="readOnly" size="small">
          <el-table-column prop="key" label="配置项" width="220" />
          <el-table-column prop="value" label="当前值" />
          <el-table-column label="生效方式" width="120">
            <template #default>
              <el-tag size="small" type="warning">需重启</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.settings {
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
  letter-spacing: -0.01em;
}

.mb16 {
  margin-bottom: 16px;
}

.weight-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.weight-row .el-slider {
  flex: 1;
}

.weight-note {
  white-space: nowrap;
  font-size: 12px;
}

.mcp-path-input {
  max-width: 320px;
}

.mcp-conn {
  margin-top: 8px;
  padding-top: 16px;
  border-top: 1px solid var(--br-border);
}

.mcp-conn-title {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
}

.mcp-conn-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.mcp-conn-label {
  width: 120px;
  flex-shrink: 0;
  font-size: 13px;
  color: var(--br-text-secondary, #666);
}

.mcp-conn-value {
  padding: 4px 8px;
  background: var(--br-bg-inset);
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-sm, 6px);
  font-size: 12px;
  word-break: break-all;
}

.mcp-conn-tools {
  align-items: flex-start;
}

.mcp-conn-tool-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.mcp-conn-example {
  margin-top: 8px;
}

.mcp-conn-example-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.mcp-conn-pre {
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
