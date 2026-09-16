<script setup lang="ts">
// 四指标汇总数字卡：值 0~1 保留 3 位 + 达标色阶；下方小字标注有效样本/覆盖率/降级口径
// 卡片沿用双层卡片 hover 抬升语言（frontend-design §6.2）
import { computed } from 'vue'
import type { EvalMetric, EvalMetricSummary } from '../../api/types'
import { METRIC_META, formatScore, scoreColor } from '../../utils/evalScore'

const props = defineProps<{
  summary: Partial<Record<EvalMetric, EvalMetricSummary>>
  metrics: EvalMetric[] // 参与评分的指标子集（任务提交时所选）
  sampleTotal?: number
}>()

const cards = computed(() =>
  props.metrics.map((m) => {
    const s = props.summary[m]
    return {
      metric: m,
      label: METRIC_META[m].label,
      en: METRIC_META[m].en,
      mean: s?.mean ?? null,
      validSamples: s?.valid_samples ?? 0,
      coverage: s?.coverage ?? 0,
      degraded: s?.degraded ?? false,
      note: s?.note ?? '',
    }
  }),
)
</script>

<template>
  <div class="metric-cards">
    <div v-for="c in cards" :key="c.metric" class="metric-card">
      <div class="metric-head">
        <span class="metric-label">{{ c.label }}</span>
        <span class="br-muted metric-en">{{ c.en }}</span>
      </div>
      <div class="metric-value" :style="{ color: scoreColor(c.mean) }">
        {{ formatScore(c.mean) }}
      </div>
      <div class="br-muted metric-foot">
        有效样本 {{ c.validSamples }}<template v-if="sampleTotal != null"> / {{ sampleTotal }}</template>
        · 覆盖率 {{ Math.round(c.coverage * 100) }}%
      </div>
      <div v-if="c.degraded || c.note" class="metric-note" :title="c.note">
        {{ c.degraded ? '降级口径' : '' }}{{ c.degraded && c.note ? ' · ' : '' }}{{ c.note }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.metric-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
}

.metric-card {
  padding: 16px 18px;
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-sm);
  transition:
    transform var(--br-transition-base),
    box-shadow var(--br-transition-base),
    border-color var(--br-transition-base);
}

.metric-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--br-shadow-md);
  border-color: var(--br-primary-soft-2);
}

.metric-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-bottom: 8px;
}

.metric-label {
  font-size: 13.5px;
  font-weight: 600;
}

.metric-en {
  font-size: 11px;
}

.metric-value {
  font-size: 28px;
  font-weight: 700;
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
  line-height: 1.2;
  margin-bottom: 6px;
}

.metric-foot {
  font-size: 12px;
}

.metric-note {
  margin-top: 4px;
  font-size: 11.5px;
  color: var(--el-color-warning);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
