<script setup lang="ts">
// 单样本下钻抽屉：Question 全文 / Answer（MarkdownRenderer）/ Ground Truth /
// Contexts（仿 SourceCard 卡片样式）/ 四指标分数条 / judge 理由折叠 / 上一条·下一条连续浏览
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowLeft, ArrowRight, Document } from '@element-plus/icons-vue'
import type { EvalMetric, EvalSampleDetail, EvalSampleRow } from '../../api/types'
import { getEvalSample } from '../../api/eval'
import { METRIC_META, METRIC_ORDER, formatScore, normalizeScore, scoreColor } from '../../utils/evalScore'
import MarkdownRenderer from '../MarkdownRenderer.vue'

const props = defineProps<{
  modelValue: boolean
  taskId: string
  row: EvalSampleRow | null
  hasPrev: boolean
  hasNext: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [v: boolean]
  navigate: [dir: 'prev' | 'next']
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const loading = ref(false)
const detail = ref<EvalSampleDetail | null>(null)

// 打开或切换样本时拉取完整内容（含 contexts 正文与判定理由）
watch(
  () => [props.modelValue, props.row?.sample_id] as const,
  async ([open, sampleId]) => {
    if (!open || !sampleId) return
    loading.value = true
    detail.value = null
    try {
      detail.value = await getEvalSample(props.taskId, sampleId)
    } catch (e) {
      ElMessage.error('加载样本详情失败：' + (e instanceof Error ? e.message : String(e)))
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

const metricBars = computed(() => {
  const d = detail.value
  if (!d) return []
  return METRIC_ORDER.filter((m) => m in (d.scores ?? {})).map((m: EvalMetric) => {
    const n = normalizeScore(d.scores[m])
    return { metric: m, label: METRIC_META[m].label, score: n.score, rationale: n.rationale }
  })
})

const hasRationale = computed(() => metricBars.value.some((b) => b.rationale))
</script>

<template>
  <el-drawer v-model="visible" size="60%" :with-header="false" class="sample-drawer">
    <div v-loading="loading" class="drawer-body">
      <template v-if="detail">
        <div class="drawer-head">
          <span class="drawer-title br-text-ellipsis" :title="detail.question">样本详情</span>
          <el-tag v-if="detail.status !== 'ok'" type="warning" size="small">样本失败</el-tag>
        </div>

        <el-alert
          v-if="detail.error"
          type="warning"
          :closable="false"
          class="drawer-alert"
          show-icon
        >
          <template #title>{{ detail.error }}</template>
        </el-alert>

        <!-- 四指标分数条 -->
        <div class="score-bars">
          <div v-for="b in metricBars" :key="b.metric" class="score-bar">
            <span class="br-muted score-bar-label">{{ b.label }}</span>
            <el-progress
              :percentage="b.score == null ? 0 : Math.round(b.score * 100)"
              :stroke-width="10"
              :color="scoreColor(b.score)"
              :show-text="false"
              class="score-bar-track"
            />
            <span class="score-bar-value" :style="{ color: scoreColor(b.score) }">
              {{ formatScore(b.score) }}
            </span>
          </div>
        </div>

        <!-- judge 判定理由（调试用，折叠） -->
        <el-collapse v-if="hasRationale" class="rationale-collapse">
          <el-collapse-item title="评审理由（judge 原始输出）" name="rationale">
            <div v-for="b in metricBars.filter((x) => x.rationale)" :key="b.metric" class="rationale-item">
              <div class="rationale-metric">{{ b.label }}</div>
              <p class="rationale-text">{{ b.rationale }}</p>
            </div>
          </el-collapse-item>
        </el-collapse>

        <div class="field-block">
          <div class="field-title">问题</div>
          <p class="field-text">{{ detail.question }}</p>
        </div>

        <div class="field-block">
          <div class="field-title">回答</div>
          <div class="field-md">
            <MarkdownRenderer :content="detail.answer || '（空回答）'" />
          </div>
        </div>

        <div v-if="detail.reference" class="field-block">
          <div class="field-title">标准答案（Ground Truth）</div>
          <p class="field-text">{{ detail.reference }}</p>
        </div>

        <div v-if="detail.contexts?.length" class="field-block">
          <div class="field-title">检索上下文 · {{ detail.contexts.length }} 条</div>
          <div class="ctx-list">
            <div v-for="(ctx, idx) in detail.contexts" :key="ctx.id || idx" class="ctx-card">
              <div class="ctx-head">
                <span class="ctx-idx">{{ idx + 1 }}</span>
                <el-icon class="ctx-icon"><Document /></el-icon>
                <span class="ctx-name br-text-ellipsis" :title="ctx.filename">{{ ctx.filename || '未知来源' }}</span>
                <span v-if="ctx.heading" class="br-muted ctx-heading br-text-ellipsis" :title="ctx.heading">
                  {{ ctx.heading }}
                </span>
                <span v-if="ctx.score != null" class="ctx-score">{{ ctx.score.toFixed(2) }}</span>
              </div>
              <pre class="ctx-content">{{ ctx.content }}</pre>
            </div>
          </div>
        </div>
      </template>
      <el-empty v-else-if="!loading" description="样本详情加载失败" :image-size="80" />

      <div class="drawer-foot">
        <el-button :icon="ArrowLeft" :disabled="!hasPrev" @click="emit('navigate', 'prev')">
          上一条
        </el-button>
        <el-button :disabled="!hasNext" @click="emit('navigate', 'next')">
          下一条
          <el-icon class="el-icon--right"><ArrowRight /></el-icon>
        </el-button>
      </div>
    </div>
  </el-drawer>
</template>

<style scoped>
.drawer-body {
  display: flex;
  flex-direction: column;
  min-height: 100%;
  padding: 4px 4px 0;
}

.drawer-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}

.drawer-title {
  font-size: 16px;
  font-weight: 700;
  letter-spacing: -0.01em;
}

.drawer-alert {
  margin-bottom: 14px;
}

.score-bars {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 16px;
  margin-bottom: 16px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-md);
  background: var(--br-bg-inset);
}

.score-bar {
  display: flex;
  align-items: center;
  gap: 12px;
}

.score-bar-label {
  width: 96px;
  flex-shrink: 0;
  font-size: 12.5px;
}

.score-bar-track {
  flex: 1;
}

.score-bar-value {
  width: 52px;
  flex-shrink: 0;
  text-align: right;
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 13px;
  font-weight: 600;
}

.rationale-collapse {
  margin-bottom: 16px;
}

.rationale-item {
  margin-bottom: 10px;
}

.rationale-metric {
  font-size: 12.5px;
  font-weight: 600;
  margin-bottom: 2px;
}

.rationale-text {
  margin: 0;
  font-size: 12.5px;
  color: var(--br-text-secondary);
  white-space: pre-wrap;
}

.field-block {
  margin-bottom: 18px;
}

.field-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--br-text-secondary);
  margin-bottom: 6px;
}

.field-text {
  margin: 0;
  font-size: 13.5px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-word;
}

.field-md {
  padding: 12px 14px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-md);
  background: var(--br-bg-inset);
}

/* 上下文卡片：仿 SourceCard 语言（编号徽章 + 图标 + 文件名 + 分数 chip） */
.ctx-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ctx-card {
  padding: 10px 12px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-md);
  background: var(--br-bg-inset);
  transition:
    border-color var(--br-transition-fast),
    background-color var(--br-transition-fast);
}

.ctx-card:hover {
  border-color: var(--br-primary-soft-2);
  background: var(--br-bg-card);
}

.ctx-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  min-width: 0;
}

.ctx-idx {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px;
  height: 20px;
  flex-shrink: 0;
  border-radius: 7px;
  background: var(--br-primary);
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.ctx-icon {
  color: var(--br-primary);
  flex-shrink: 0;
}

.ctx-name {
  font-size: 13px;
  min-width: 0;
}

.ctx-heading {
  font-size: 12px;
  min-width: 0;
}

.ctx-score {
  flex-shrink: 0;
  margin-left: auto;
  font-size: 11px;
  font-weight: 600;
  color: var(--br-text-secondary);
  font-variant-numeric: tabular-nums;
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  border-radius: 5px;
  padding: 1px 6px;
}

.ctx-content {
  margin: 0;
  max-height: 160px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: 12.5px;
  line-height: 1.65;
  color: var(--br-text-secondary);
}

.drawer-foot {
  display: flex;
  justify-content: space-between;
  margin-top: auto;
  padding: 14px 0 4px;
  border-top: 1px solid var(--br-border);
}
</style>
