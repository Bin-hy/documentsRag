<script setup lang="ts">
// 多任务对比（P2）：并排汇总表（最优 ▲ 标记 / 差异 >0.05 着色）+ 叠加雷达图（≤4）+ 分组柱状图
// + config_snapshot 三要素（dataset_hash / judge_model / prompt_version）可比性警告
// 进入方式：列表页勾选跳转 ?ids=a,b（可分享链接，URL 为单一事实源）
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { getEvalReportsBatch } from '../../api/eval'
import type { EvalCompareItem, EvalMetric } from '../../api/types'
import { METRIC_META, METRIC_ORDER, formatScore, scoreColor } from '../../utils/evalScore'
import { buildCompareBarOption, buildRadarOption } from '../../utils/evalChart'

const EvalChart = defineAsyncComponent(() => import('../../components/eval/EvalChart.vue'))

const route = useRoute()
const router = useRouter()

// 契约上限 8；雷达叠加超过 4 个视觉过密，雷达只取前 4（frontend-design §5.3）
const ids = computed(() =>
  String(route.query.ids ?? '')
    .split(',')
    .filter(Boolean)
    .slice(0, 8),
)

const items = ref<EvalCompareItem[]>([])
const loading = ref(false)
const loadError = ref('')

async function load() {
  if (ids.value.length < 2) return
  loading.value = true
  loadError.value = ''
  try {
    const res = await getEvalReportsBatch(ids.value)
    // 按 URL 顺序排列
    const byId = new Map((res?.items ?? []).map((it) => [it.task_id, it]))
    items.value = ids.value.map((id) => byId.get(id)).filter((x): x is EvalCompareItem => !!x)
  } catch (e) {
    loadError.value = (e as Error).message
    ElMessage.error('加载对比数据失败：' + loadError.value)
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(ids, load)

// —— 可比性警告：三要素任一不一致 → 警告而非阻止（architect-design §6.2）——
const SNAPSHOT_KEYS = [
  { key: 'dataset_hash', label: '数据集版本' },
  { key: 'judge_model', label: '评审模型' },
  { key: 'prompt_version', label: '提示词版本' },
] as const

const comparabilityWarnings = computed(() => {
  if (items.value.length < 2) return []
  const warns: string[] = []
  for (const { key, label } of SNAPSHOT_KEYS) {
    const values = new Set(items.value.map((it) => it.config_snapshot?.[key] ?? ''))
    if (values.size > 1) warns.push(`${label}不一致`)
  }
  return warns
})

// —— 并排汇总表数据 ——
const DIFF_THRESHOLD = 0.05 // 差异超过阈值着色提示

interface MetricRow {
  metric: EvalMetric
  label: string
  means: Array<number | null>
  bestIdx: number // 最高分列（-1 = 全空）
}

const metricRows = computed<MetricRow[]>(() =>
  METRIC_ORDER.map((m) => {
    const means = items.value.map((it) => it.summary[m]?.mean ?? null)
    let bestIdx = -1
    let best = -Infinity
    means.forEach((v, i) => {
      if (v != null && v > best) {
        best = v
        bestIdx = i
      }
    })
    return { metric: m, label: METRIC_META[m].label, means, bestIdx }
  }).filter((r) => r.means.some((v) => v != null)),
)

/** 与最优差超过阈值 → 着色提示 */
function isDivergent(row: MetricRow, idx: number): boolean {
  const v = row.means[idx]
  const best = row.bestIdx >= 0 ? row.means[row.bestIdx] : null
  return v != null && best != null && best - v > DIFF_THRESHOLD
}

// —— 图表 ——
const radarSeries = computed(() =>
  items.value.slice(0, 4).map((it) => ({
    name: it.name,
    values: METRIC_ORDER.map((m) => it.summary[m]?.mean ?? null),
  })),
)

function makeRadarOption(isDark: boolean) {
  return buildRadarOption([...METRIC_ORDER], radarSeries.value, isDark)
}

function makeBarOption(isDark: boolean) {
  return buildCompareBarOption([...METRIC_ORDER], radarSeries.value, isDark)
}
</script>

<template>
  <div class="eval-compare-page">
    <div class="page-head">
      <div class="head-left">
        <el-button text :icon="ArrowLeft" @click="router.push('/eval')">返回列表</el-button>
        <h2 class="page-title">评测对比</h2>
      </div>
    </div>

    <el-empty
      v-if="ids.length < 2"
      description="请从评测列表勾选 2~4 个已完成任务后发起对比"
    >
      <el-button type="primary" @click="router.push('/eval')">去选择任务</el-button>
    </el-empty>

    <template v-else>
      <el-alert v-if="loadError" type="error" :title="loadError" :closable="false" show-icon />

      <!-- 可比性警告条 -->
      <el-alert
        v-if="comparabilityWarnings.length"
        type="warning"
        :closable="false"
        show-icon
        class="compare-alert"
      >
        <template #title>
          对比口径不一致：{{ comparabilityWarnings.join('、') }}。分数仍可参考，但口径差异需注意。
        </template>
      </el-alert>

      <div v-loading="loading">
        <!-- 并排汇总表：行=指标/元信息，列=任务 -->
        <div v-if="items.length" class="panel-card section">
          <div class="section-title">汇总对比</div>
          <div class="compare-table-wrap">
            <table class="compare-table">
              <thead>
                <tr>
                  <th class="col-label">指标 / 元信息</th>
                  <th v-for="it in items" :key="it.task_id">
                    <div class="task-name" :title="it.name">{{ it.name }}</div>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in metricRows" :key="row.metric">
                  <td class="col-label">{{ row.label }}</td>
                  <td
                    v-for="(it, idx) in items"
                    :key="it.task_id"
                    :class="{ 'cell-divergent': isDivergent(row, idx) }"
                  >
                    <span class="cell-score" :style="{ color: scoreColor(row.means[idx]) }">
                      {{ formatScore(row.means[idx]) }}
                    </span>
                    <span v-if="idx === row.bestIdx" class="cell-best">▲ 最优</span>
                  </td>
                </tr>
                <tr>
                  <td class="col-label">有效样本</td>
                  <td v-for="it in items" :key="it.task_id">
                    <span class="mono">
                      {{ Math.max(0, ...Object.values(it.summary).map((s) => s?.valid_samples ?? 0)) || '—' }}
                    </span>
                  </td>
                </tr>
                <tr>
                  <td class="col-label">评审模型</td>
                  <td v-for="it in items" :key="it.task_id">
                    <span class="mono">{{ it.config_snapshot?.judge_model || '—' }}</span>
                  </td>
                </tr>
                <tr>
                  <td class="col-label">提示词版本</td>
                  <td v-for="it in items" :key="it.task_id">
                    <span class="mono">{{ it.config_snapshot?.prompt_version || '—' }}</span>
                  </td>
                </tr>
                <tr>
                  <td class="col-label">完成时间</td>
                  <td v-for="it in items" :key="it.task_id">
                    <span class="br-muted">
                      {{ it.finished_at ? new Date(it.finished_at).toLocaleString() : '—' }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- 叠加雷达图（≤4） -->
        <div v-if="items.length" class="panel-card section">
          <div class="section-title">
            指标雷达叠加
            <span v-if="items.length > 4" class="br-muted section-hint">（仅展示前 4 个任务）</span>
          </div>
          <EvalChart :make-option="makeRadarOption" height="380px" />
        </div>

        <!-- 分组柱状图 -->
        <div v-if="items.length" class="panel-card section">
          <div class="section-title">分组柱状对比</div>
          <EvalChart :make-option="makeBarOption" height="340px" />
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.eval-compare-page {
  padding: 24px 28px;
}

.page-head {
  margin-bottom: 20px;
}

.head-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.page-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.compare-alert {
  margin-bottom: 16px;
}

.panel-card {
  padding: 20px 22px;
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-sm);
}

.section {
  margin-bottom: 16px;
}

.section-title {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.01em;
  margin-bottom: 14px;
}

.section-hint {
  font-size: 12px;
  font-weight: 400;
}

.compare-table-wrap {
  overflow-x: auto;
}

.compare-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.compare-table th,
.compare-table td {
  padding: 10px 14px;
  text-align: left;
  border-bottom: 1px solid var(--br-border);
}

.compare-table thead th {
  font-weight: 600;
  color: var(--br-text-secondary);
  border-bottom: 1px solid var(--br-border-strong);
}

.compare-table tbody tr:hover td {
  background: var(--br-bg-hover);
}

.col-label {
  width: 140px;
  color: var(--br-text-secondary);
  font-weight: 500;
  white-space: nowrap;
}

.task-name {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--br-text);
}

.cell-score {
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}

.cell-best {
  margin-left: 8px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--el-color-success);
}

.cell-divergent {
  background: color-mix(in srgb, var(--el-color-warning) 9%, transparent);
}

.mono {
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 12.5px;
}
</style>
