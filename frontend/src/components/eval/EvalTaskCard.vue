<script setup lang="ts">
// 评测任务卡片：沿用 KbListView 双层卡片语言（hover 抬升 + 阴影 + 边框变品牌色）
// 内容：状态 tag、进度条（运行中）、四指标迷你值（完成后且后端返回 summary 时）、创建时间/耗时、操作
import { computed } from 'vue'
import { Delete, View } from '@element-plus/icons-vue'
import type { EvalTask } from '../../api/types'
import {
  STATUS_META,
  formatDuration,
  formatScore,
  isActiveStatus,
  METRIC_META,
  METRIC_ORDER,
  scoreColor,
} from '../../utils/evalScore'

const props = defineProps<{
  task: EvalTask
  kbName?: string
  datasetName?: string
  checked?: boolean
  /** 对比勾选禁用原因（非 completed 或已选满 4 个），空串 = 可勾选 */
  checkDisabledReason?: string
}>()

const emit = defineEmits<{
  enter: []
  remove: []
  toggleCheck: [checked: boolean]
}>()

const statusMeta = computed(() => STATUS_META[props.task.status])

const running = computed(() => isActiveStatus(props.task.status))

// 进度百分比：采集阶段看 collected，评分阶段看 evaluated
const percent = computed(() => {
  const p = props.task.progress
  if (!p || p.total <= 0) return 0
  const done = props.task.status === 'collecting' ? p.collected : p.evaluated
  return Math.min(100, Math.round((done / p.total) * 100))
})

const progressText = computed(() => {
  const p = props.task.progress
  if (!p) return ''
  if (props.task.status === 'collecting') return `已采集 ${p.collected} / 共 ${p.total} 条`
  if (props.task.status === 'evaluating') return `已评 ${p.evaluated} / 共 ${p.total} 条`
  return `共 ${p.total} 条样本`
})

// 耗时：终态用 finished_at-started_at/created_at；运行中用到当前
const durationText = computed(() => {
  const t = props.task
  const start = t.started_at || t.created_at
  if (!start) return '—'
  const end = t.finished_at || (running.value ? new Date().toISOString() : null)
  if (!end) return '—'
  return formatDuration(new Date(end).getTime() - new Date(start).getTime())
})

const metricEntries = computed(() => {
  const s = props.task.summary
  if (!s) return []
  return METRIC_ORDER.filter((m) => s[m] != null).map((m) => ({
    metric: m,
    label: METRIC_META[m].label,
    value: s[m] as number,
  }))
})
</script>

<template>
  <div class="eval-card" @click="emit('enter')">
    <div class="eval-card-head">
      <el-tooltip :content="checkDisabledReason" :disabled="!checkDisabledReason" placement="top">
        <span class="eval-check" @click.stop>
          <el-checkbox
            :model-value="checked"
            :disabled="!!checkDisabledReason"
            @change="(v: boolean) => emit('toggleCheck', v)"
          />
        </span>
      </el-tooltip>
      <span class="eval-name br-text-ellipsis" :title="task.name">{{ task.name }}</span>
      <el-tag :type="statusMeta.tagType" size="small" effect="light" class="eval-status">
        {{ statusMeta.label }}
      </el-tag>
    </div>

    <div class="eval-meta br-muted">
      <span class="br-text-ellipsis">{{ kbName || '不限定知识库' }}</span>
      <span class="eval-dot">·</span>
      <span class="br-text-ellipsis">{{ datasetName || task.dataset_id }}</span>
      <span class="eval-dot">·</span>
      <span class="br-text-ellipsis">评审 {{ task.judge_model }}</span>
    </div>

    <div v-if="running" class="eval-progress">
      <el-progress :percentage="percent" :stroke-width="8" striped striped-flow />
      <span class="br-muted eval-progress-text">
        {{ progressText }}<template v-if="task.progress?.failed"> · 失败 {{ task.progress.failed }}</template>
      </span>
    </div>

    <div v-if="metricEntries.length" class="eval-metrics">
      <span v-for="e in metricEntries" :key="e.metric" class="eval-metric">
        <span class="br-muted eval-metric-label">{{ e.label }}</span>
        <span class="eval-metric-value" :style="{ color: scoreColor(e.value) }">
          {{ formatScore(e.value) }}
        </span>
      </span>
    </div>

    <div v-if="task.status === 'failed' && task.error_message" class="eval-error br-text-ellipsis" :title="task.error_message">
      {{ task.error_message }}
    </div>

    <div class="eval-card-foot">
      <span class="br-muted eval-date">
        {{ new Date(task.created_at).toLocaleString() }} · 耗时 {{ durationText }}
      </span>
      <div class="eval-actions" @click.stop>
        <el-button size="small" text :icon="View" @click="emit('enter')">详情</el-button>
        <el-button size="small" text type="danger" :icon="Delete" @click="emit('remove')">删除</el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.eval-card {
  margin-bottom: 16px;
  padding: 16px;
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-sm);
  cursor: pointer;
  transition:
    transform var(--br-transition-base),
    box-shadow var(--br-transition-base),
    border-color var(--br-transition-base);
}

.eval-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--br-shadow-md);
  border-color: var(--br-primary-soft-2);
}

.eval-card:active {
  transform: translateY(-1px) scale(0.995);
}

.eval-card-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.eval-check {
  display: inline-flex;
  flex-shrink: 0;
}

.eval-name {
  flex: 1;
  min-width: 0;
  font-size: 15px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.eval-status {
  flex-shrink: 0;
}

.eval-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  margin-bottom: 10px;
  min-width: 0;
}

.eval-meta > span {
  min-width: 0;
}

.eval-dot {
  flex-shrink: 0;
}

.eval-progress {
  margin-bottom: 10px;
}

.eval-progress-text {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  font-family: var(--br-font-mono);
}

.eval-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 10px;
}

.eval-metric {
  display: inline-flex;
  align-items: baseline;
  gap: 5px;
}

.eval-metric-label {
  font-size: 12px;
}

.eval-metric-value {
  font-size: 13px;
  font-weight: 600;
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
}

.eval-error {
  margin-bottom: 10px;
  font-size: 12px;
  color: var(--el-color-danger);
}

.eval-card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-top: 12px;
  border-top: 1px solid var(--br-border);
}

.eval-date {
  font-size: 12px;
}
</style>
