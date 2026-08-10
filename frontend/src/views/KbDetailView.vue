<script setup lang="ts">
// 知识库详情：信息头 + 策略设置 + 上传面板 + 文档/任务列表
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useDocStore } from '../stores/doc'
import * as kbApi from '../api/kb'
import type { StrategyConfig } from '../api/types'
import UploadPanel from '../components/UploadPanel.vue'
import TaskList from '../components/TaskList.vue'

const route = useRoute()
const router = useRouter()
const docStore = useDocStore()

const kbId = route.params.id as string
const kbName = ref('')
const kbDescription = ref('')
const saving = ref(false)

// 策略表单（空 = 继承全局）
const strategy = ref<StrategyConfig>({})

function parseStrategy(raw: string): StrategyConfig {
  if (!raw) return {}
  try {
    return JSON.parse(raw) as StrategyConfig
  } catch {
    return {}
  }
}

async function saveStrategy() {
  saving.value = true
  try {
    await kbApi.updateKb(kbId, kbName.value, kbDescription.value, strategy.value)
    ElMessage.success('策略已保存')
  } catch (e) {
    ElMessage.error('保存失败：' + (e instanceof Error ? e.message : String(e)))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  // 知识库信息：优先取 store（列表页已加载）；直接刷新时兜底加载
  const { useKbStore } = await import('../stores/kb')
  const kbStore = useKbStore()
  if (kbStore.kbs.length === 0) {
    await kbStore.load()
  }
  const kb = kbStore.kbs.find((k) => k.ID === kbId)
  if (!kb) {
    router.replace('/kb')
    return
  }
  kbName.value = kb.Name
  kbDescription.value = kb.Description
  strategy.value = parseStrategy(kb.Strategy)
  docStore.load(kbId)
  docStore.startPolling()
})

onUnmounted(() => {
  docStore.stopPolling()
})
</script>

<template>
  <div class="kb-detail">
    <div class="detail-head">
      <el-button text :icon="ArrowLeft" class="back-btn" @click="router.push('/kb')">返回</el-button>
      <div class="detail-info">
        <h2 class="detail-name">{{ kbName }}</h2>
        <p v-if="kbDescription" class="detail-desc">{{ kbDescription }}</p>
      </div>
    </div>

    <div class="detail-body">
      <div class="strategy-section">
        <div class="strategy-head">
          <h3 class="strategy-title">检索策略</h3>
          <span class="br-muted">留空 = 使用全局默认（可参考 config.yaml 的 rag.strategy）</span>
        </div>
        <el-form label-width="90px" label-position="left" class="strategy-form">
          <el-form-item label="查询模式">
            <el-radio-group v-model="strategy.query">
              <el-radio value="multi">多查询（多路召回）</el-radio>
              <el-radio value="single">单查询（低延迟）</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="问题分解">
            <el-radio-group v-model="strategy.decomposition">
              <el-radio value="off">关闭</el-radio>
              <el-radio value="parallel">并行分解</el-radio>
              <el-radio value="sequential">顺序分解</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="回退查询">
            <el-switch v-model="strategy.step_back" active-value="on" inactive-value="off" />
          </el-form-item>
          <el-form-item label="HyDE">
            <el-switch v-model="strategy.hyde" active-value="on" inactive-value="off" />
          </el-form-item>
          <el-form-item label="自动路由">
            <el-switch v-model="strategy.routing" active-value="auto" inactive-value="off" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="saving" @click="saveStrategy">保存策略</el-button>
          </el-form-item>
        </el-form>
      </div>

      <UploadPanel :kb-id="kbId" />

      <div class="doc-section">
        <div class="doc-head">
          <h3 class="doc-title">文档与任务（{{ docStore.documents.length }}）</h3>
          <span v-if="docStore.hasActiveTasks" class="br-muted">任务处理中，列表自动刷新…</span>
        </div>
        <TaskList :documents="docStore.documents" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.kb-detail {
  padding: 20px 28px;
}

.detail-head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-bottom: 20px;
}

.back-btn {
  margin-top: 2px;
  border-radius: var(--br-radius-pill);
}

.detail-info {
  flex: 1;
}

.detail-name {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.detail-desc {
  margin: 6px 0 0;
  color: var(--br-text-secondary);
  font-size: 13.5px;
}

/* 卡片化表面 */
.strategy-section {
  margin-bottom: 18px;
  padding: 18px 20px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  box-shadow: var(--br-shadow-sm);
}

.strategy-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.strategy-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.strategy-form {
  max-width: 560px;
}

.doc-section {
  margin-top: 22px;
}

.doc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.doc-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
}
</style>
