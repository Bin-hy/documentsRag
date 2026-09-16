<script setup lang="ts">
// 任务详情：运行中=进度面板（状态卡 + 阶段步骤条 + 进度条 + 取消）；完成=报告
// （汇总卡 + 雷达/分布图 + 明细表 + Drawer 下钻）；失败=错误卡 + 重新发起（预填）
// 轮询由 store 统一管理（链式退避 + 可见性 + 终态自停），本页 unmount 必停。
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, RefreshRight, VideoPause, WarningFilled } from '@element-plus/icons-vue'
import { useEvalStore } from '../../stores/eval'
import { getEvalReport } from '../../api/eval'
import type { EvalMetric, EvalReportQuery, EvalSampleRow } from '../../api/types'
import {
  METRIC_ORDER,
  STATUS_META,
  formatDuration,
  isActiveStatus,
} from '../../utils/evalScore'
import { buildDistributionOption, buildRadarOption } from '../../utils/evalChart'
import MetricSummaryCards from '../../components/eval/MetricSummaryCards.vue'
import EvalSampleTable from '../../components/eval/EvalSampleTable.vue'
import EvalSampleDrawer from '../../components/eval/EvalSampleDrawer.vue'

// 图表懒加载：运行中形态不下载 ECharts（frontend-design §2.4）
const EvalChart = defineAsyncComponent(() => import('../../components/eval/EvalChart.vue'))

const route = useRoute()
const router = useRouter()
const evalStore = useEvalStore()

const taskId = route.params.id as string
const loading = ref(true)
const loadError = ref('')

const task = computed(() => evalStore.currentTask)
const report = computed(() => evalStore.currentReport)
const running = computed(() => !!task.value && isActiveStatus(task.value.status))
const statusMeta = computed(() => (task.value ? STATUS_META[task.value.status] : null))
const reportVisible = computed(
  () => !!task.value && (task.value.status === 'completed' || (task.value.status === 'canceled' && !!task.value.report_ready)) && !!report.value,
)

// 阶段步骤条：收集回答 → RAGAS 评分 → 生成报告（status/stage 映射，frontend-design §2.3 形态 A）
const stageIndex = computed(() => {
  switch (task.value?.status) {
    case 'collecting':
      return 0
    case 'evaluating':
      return 1
    default:
      return 0 // pending：排队中，停在第一步
  }
})

// 进度：采集阶段看 collected，评分阶段看 evaluated
const progressDone = computed(() => {
  const t = task.value
  if (!t?.progress) return 0
  return t.status === 'collecting' ? t.progress.collected : t.progress.evaluated
})
const progressPercent = computed(() => {
  const p = task.value?.progress
  if (!p || p.total <= 0) return 0
  return Math.min(100, Math.round((progressDone.value / p.total) * 100))
})

// 已运行时长（随轮询刷新）
const elapsedText = computed(() => {
  const t = task.value
  if (!t) return '—'
  const start = t.started_at || t.created_at
  const end = t.finished_at || new Date().toISOString()
  return formatDuration(new Date(end).getTime() - new Date(start).getTime())
})

// —— 图表 option 工厂（EvalChart 会在 isDark/依赖变化时重新求值）——
const chartTab = ref<'radar' | 'dist'>('radar')

// 契约未含逐样本分布字段：优先用 report.distribution，缺省用一页样本（≤100）在前端分桶近似
const distScores = ref<Partial<Record<EvalMetric, number[]>>>({})

const radarMetrics = computed<EvalMetric[]>(
  () => report.value?.task.metrics.filter((m) => METRIC_ORDER.includes(m)) ?? [],
)

function makeRadarOption(isDark: boolean) {
  const r = report.value
  const metrics = radarMetrics.value
  return buildRadarOption(
    metrics,
    [
      {
        name: r?.task.name ?? '',
        values: metrics.map((m) => r?.summary[m]?.mean ?? null),
      },
    ],
    isDark,
  )
}

function makeDistOption(isDark: boolean) {
  const r = report.value
  const metrics = radarMetrics.value
  return buildDistributionOption(metrics, r?.distribution ?? distScores.value, isDark)
}

async function loadDistribution() {
  if (report.value?.distribution) return // 后端已提供则无需近似
  try {
    // 拉一页大 page_size 样本做前端分桶（契约 page_size 上限 100）
    const r = await getEvalReport(taskId, { page: 1, page_size: 100 })
    const dist: Partial<Record<EvalMetric, number[]>> = {}
    for (const m of radarMetrics.value) {
      dist[m] = r.samples.items
        .map((s) => s.scores[m])
        .filter((v): v is number => typeof v === 'number')
    }
    distScores.value = dist
  } catch {
    /* 分布图为增强视图，失败不阻断主报告 */
  }
}

// —— 明细表与下钻抽屉 ——
const drawerVisible = ref(false)
const drawerIndex = ref(-1)

const sampleRows = computed(() => report.value?.samples.items ?? [])
const drawerRow = computed<EvalSampleRow | null>(() => sampleRows.value[drawerIndex.value] ?? null)

function openDrawer(row: EvalSampleRow) {
  drawerIndex.value = sampleRows.value.findIndex((r) => r.sample_id === row.sample_id)
  drawerVisible.value = true
}

function navigateDrawer(dir: 'prev' | 'next') {
  const next = drawerIndex.value + (dir === 'next' ? 1 : -1)
  if (next < 0 || next >= sampleRows.value.length) return
  drawerIndex.value = next
}

function onTableQuery(q: EvalReportQuery) {
  evalStore.loadReport(taskId, q).catch((e) => ElMessage.error((e as Error).message))
}

// —— 操作 ——
const canceling = ref(false)

async function cancelTask() {
  try {
    await ElMessageBox.confirm('确定取消该评测任务吗？已产出的部分结果将保留。', '取消任务', {
      confirmButtonText: '取消任务',
      cancelButtonText: '再想想',
      type: 'warning',
    })
  } catch {
    return
  }
  canceling.value = true
  try {
    await evalStore.cancelTask(taskId)
    ElMessage.success('任务已取消')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    canceling.value = false
  }
}

/** 失败重发：携带原参数预填 /eval/new 表单（草稿存 store） */
function relaunch() {
  if (!task.value) return
  evalStore.draftFromTask(task.value)
  router.push('/eval/new')
}

async function loadReportAndDist() {
  try {
    await evalStore.loadReport(taskId, { page: 1 })
    await loadDistribution()
  } catch (e) {
    ElMessage.error('加载报告失败：' + (e as Error).message)
  }
}

// 轮询把任务推到终态后补拉报告（store 已拉一次 completed；canceled+report_ready 在此兜底）
watch(
  () => task.value?.status,
  (s, prev) => {
    if (s && s !== prev && !isActiveStatus(s) && (s === 'completed' || task.value?.report_ready)) {
      if (!report.value) void loadReportAndDist()
    }
  },
)

onMounted(async () => {
  loading.value = true
  try {
    const t = await evalStore.fetchTask(taskId)
    if (isActiveStatus(t.status)) {
      evalStore.startPolling(taskId)
    }
    if (t.status === 'completed' || (t.status === 'canceled' && t.report_ready)) {
      await loadReportAndDist()
    }
  } catch (e) {
    loadError.value = (e as Error).message
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  evalStore.stopPolling() // 路由切走必停
  evalStore.currentTask = null
  evalStore.currentReport = null
})
</script>

<template>
  <div class="eval-detail-page" v-loading="loading">
    <div class="page-head">
      <div class="head-left">
        <el-button text :icon="ArrowLeft" @click="router.push('/eval')">返回列表</el-button>
        <h2 class="page-title br-text-ellipsis" :title="task?.name">{{ task?.name || '任务详情' }}</h2>
        <el-tag v-if="statusMeta" :type="statusMeta.tagType" effect="light">{{ statusMeta.label }}</el-tag>
      </div>
    </div>

    <el-alert v-if="loadError" type="error" :title="loadError" :closable="false" show-icon />

    <!-- 形态 A：运行中 / 排队中（进度面板） -->
    <template v-if="task && running">
      <div class="panel-card status-card">
        <div class="status-main">
          <el-tag :type="statusMeta!.tagType" size="large" effect="dark" class="status-tag">
            {{ statusMeta!.label }}
          </el-tag>
          <div class="status-sub br-muted">
            已运行 <b class="mono">{{ elapsedText }}</b>
            <template v-if="task.progress">
              · 共 <b class="mono">{{ task.progress.total }}</b> 条样本
              <template v-if="task.progress.failed">
                · <span class="failed-count">失败 {{ task.progress.failed }}</span>
              </template>
            </template>
          </div>
        </div>
        <el-progress
          :percentage="progressPercent"
          :stroke-width="12"
          striped
          striped-flow
          class="status-progress"
        />
        <div class="br-muted progress-text">
          {{ task.status === 'collecting' ? '已采集' : '已评' }}
          {{ progressDone }} / 共 {{ task.progress?.total ?? 0 }} 条
        </div>

        <el-steps :active="stageIndex" align-center process-status="process" class="stage-steps">
          <el-step title="收集回答" description="回调 /api/v1/chat 采集回答与上下文" />
          <el-step title="RAGAS 评分" description="四指标逐样本评分" />
          <el-step title="生成报告" description="汇总指标与明细分页" />
        </el-steps>

        <el-alert
          v-if="evalStore.pollPaused"
          type="warning"
          :closable="false"
          show-icon
          class="poll-alert"
        >
          <template #title>
            进度刷新失败，已暂停自动刷新
            <el-button size="small" text type="primary" :icon="RefreshRight" @click="evalStore.resumePolling()">
              重试刷新
            </el-button>
          </template>
        </el-alert>

        <div class="status-actions">
          <el-button type="danger" plain :icon="VideoPause" :loading="canceling" @click="cancelTask">
            取消任务
          </el-button>
        </div>
      </div>
    </template>

    <!-- 失败态：错误卡 + 重新发起 -->
    <template v-if="task?.status === 'failed'">
      <div class="panel-card failed-card">
        <div class="failed-head">
          <el-icon :size="22" class="failed-icon"><WarningFilled /></el-icon>
          <span class="failed-title">评测失败</span>
          <span class="br-muted">阶段：{{ task.stage || task.status }}</span>
        </div>
        <p class="failed-message">{{ task.error_message || '未知错误' }}</p>
        <el-button type="primary" @click="relaunch">重新发起（携带原参数）</el-button>
      </div>
    </template>

    <!-- 已取消提示（部分数据语义） -->
    <el-alert
      v-if="task?.status === 'canceled'"
      type="warning"
      :closable="false"
      show-icon
      class="cancel-alert"
      title="任务已取消，下方结果为已产出样本的部分数据"
    />

    <!-- 形态 B：报告（已完成 / 已取消但报告就绪） -->
    <template v-if="reportVisible && report">
      <MetricSummaryCards
        :summary="report.summary"
        :metrics="radarMetrics"
        :sample-total="report.samples.total"
        class="report-section"
      />

      <div class="panel-card report-section">
        <el-tabs v-model="chartTab">
          <el-tab-pane label="指标雷达" name="radar" />
          <el-tab-pane label="分数分布" name="dist" />
        </el-tabs>
        <EvalChart v-if="chartTab === 'radar'" :make-option="makeRadarOption" height="340px" />
        <EvalChart v-else :make-option="makeDistOption" height="340px" />
        <div v-if="chartTab === 'dist' && !report.distribution" class="br-muted dist-hint">
          分布由前 100 条样本近似分桶（后端契约暂未提供全量分布字段）
        </div>
      </div>

      <div class="panel-card report-section">
        <div class="section-title">逐样本明细</div>
        <EvalSampleTable
          :report="report"
          :loading="evalStore.reportLoading"
          @query="onTableQuery"
          @detail="openDrawer"
        />
      </div>

      <EvalSampleDrawer
        v-model="drawerVisible"
        :task-id="taskId"
        :row="drawerRow"
        :has-prev="drawerIndex > 0"
        :has-next="drawerIndex >= 0 && drawerIndex < sampleRows.length - 1"
        @navigate="navigateDrawer"
      />
    </template>
  </div>
</template>

<style scoped>
.eval-detail-page {
  padding: 24px 28px;
}

.page-head {
  margin-bottom: 20px;
}

.head-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.page-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
  min-width: 0;
}

.panel-card {
  padding: 20px 22px;
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-sm);
}

.status-card {
  max-width: 860px;
}

.status-main {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.status-tag {
  font-size: 14px;
}

.status-sub {
  font-size: 13px;
}

.mono {
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
}

.failed-count {
  color: var(--el-color-warning);
  font-family: var(--br-font-mono);
}

.status-progress {
  margin-bottom: 6px;
}

.progress-text {
  font-size: 12.5px;
  font-family: var(--br-font-mono);
  margin-bottom: 22px;
}

.stage-steps {
  margin-bottom: 18px;
}

.poll-alert {
  margin-bottom: 16px;
}

.status-actions {
  display: flex;
  justify-content: flex-end;
}

.failed-card {
  max-width: 860px;
  border-color: color-mix(in srgb, var(--el-color-danger) 35%, var(--br-border));
}

.failed-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.failed-icon {
  color: var(--el-color-danger);
}

.failed-title {
  font-size: 16px;
  font-weight: 700;
}

.failed-message {
  margin: 0 0 16px;
  font-size: 13px;
  color: var(--br-text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}

.cancel-alert {
  margin-bottom: 16px;
}

.report-section {
  margin-bottom: 16px;
}

.section-title {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.01em;
  margin-bottom: 14px;
}

.dist-hint {
  font-size: 12px;
  margin-top: 6px;
  text-align: center;
}
</style>
