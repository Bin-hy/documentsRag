<script setup lang="ts">
// 系统配置管理：可修改组（LLM/Retriever/Strategy/Loader）+ 只读组（需重启生效）
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getConfig, updateConfig } from '../api/config'
import type { ConfigView } from '../api/types'

const loading = ref(true)
const saving = ref(false)
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
  padding: 16px 24px;
}

.page-title {
  margin: 0 0 16px;
  font-size: 20px;
}

.section {
  margin-bottom: 24px;
}

.section-title {
  margin: 0 0 12px;
  font-size: 15px;
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
</style>
