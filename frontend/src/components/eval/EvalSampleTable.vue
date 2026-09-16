<script setup lang="ts">
// 逐样本明细表：el-table + 后端分页（P0 降级方案；el-table-v2 虚拟滚动作为性能优化后补，frontend-design §5.2）
// 排序/低分过滤必须走后端（在全量数据上做），前端只做当前页渲染
import { computed, ref } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'
import type { EvalMetric, EvalReport, EvalReportQuery, EvalSampleRow } from '../../api/types'
import { METRIC_META, formatScore, scoreColor, scoreSoftBg } from '../../utils/evalScore'

const props = defineProps<{
  report: EvalReport
  loading: boolean
}>()

const emit = defineEmits<{
  query: [q: EvalReportQuery]
  detail: [row: EvalSampleRow]
}>()

// 参与评分的指标列（按任务所选子集，固定展示顺序）
const metricCols = computed(() => props.report.task.metrics ?? [])

const page = ref(props.report.samples.page || 1)
const pageSize = ref(props.report.samples.page_size || 50)
const sort = ref('') // 契约格式 "metric:asc|desc"
const metricLt = ref('') // 低分过滤 chip："metric:0.6"

function emitQuery() {
  emit('query', {
    page: page.value,
    page_size: pageSize.value,
    sort: sort.value || undefined,
    metric_lt: metricLt.value || undefined,
  })
}

function onSortChange(e: { prop?: string; order?: 'ascending' | 'descending' | null }) {
  sort.value = e.prop && e.order ? `${e.prop}:${e.order === 'ascending' ? 'asc' : 'desc'}` : ''
  page.value = 1
  emitQuery()
}

function toggleLowChip(m: EvalMetric) {
  const key = `${m}:0.6`
  metricLt.value = metricLt.value === key ? '' : key
  page.value = 1
  emitQuery()
}

function onPageChange(p: number) {
  page.value = p
  emitQuery()
}

function onSizeChange(s: number) {
  pageSize.value = s
  page.value = 1
  emitQuery()
}

// 行号 = 后端分页全局序号
function rowIndex(idx: number): number {
  return (props.report.samples.page - 1) * props.report.samples.page_size + idx + 1
}
</script>

<template>
  <div class="sample-table">
    <!-- 低分快捷筛选 chip（调优最高频操作：只看某指标 < 0.6 的样本） -->
    <div class="filter-chips">
      <span class="br-muted chips-label">低分筛选</span>
      <el-check-tag
        v-for="m in metricCols"
        :key="m"
        :checked="metricLt === `${m}:0.6`"
        class="chip"
        @change="toggleLowChip(m)"
      >
        {{ METRIC_META[m].label }} &lt; 0.6
      </el-check-tag>
      <span v-if="metricLt" class="br-muted chips-hint">已过滤，再次点击取消</span>
    </div>

    <el-table
      :data="report.samples.items"
      v-loading="loading"
      @sort-change="onSortChange"
      class="eval-table"
    >
      <el-table-column label="#" width="64" align="right">
        <template #default="{ $index }">
          <span class="cell-idx">{{ rowIndex($index) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="问题" min-width="220">
        <template #default="{ row }">
          <span class="cell-text cell-clamp" :title="row.question">{{ row.question }}</span>
        </template>
      </el-table-column>
      <el-table-column label="回答摘要" min-width="220">
        <template #default="{ row }">
          <span class="cell-text cell-clamp br-muted" :title="row.answer_excerpt">
            {{ row.answer_excerpt || '—' }}
          </span>
        </template>
      </el-table-column>
      <el-table-column
        v-for="m in metricCols"
        :key="m"
        :prop="m"
        :label="METRIC_META[m].label"
        width="118"
        align="right"
        sortable="custom"
      >
        <template #default="{ row }">
          <span
            class="cell-score"
            :style="{ color: scoreColor(row.scores[m]), background: scoreSoftBg(row.scores[m]) }"
          >
            {{ formatScore(row.scores[m]) }}
          </span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="72" align="center">
        <template #default="{ row }">
          <el-tooltip v-if="row.status !== 'ok'" :content="row.error || '样本失败'" placement="top">
            <el-icon class="cell-warn"><WarningFilled /></el-icon>
          </el-tooltip>
          <span v-else class="br-muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="80" align="center" fixed="right">
        <template #default="{ row }">
          <el-button size="small" text type="primary" @click="emit('detail', row)">详情</el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :image-size="80" :description="metricLt ? '没有符合低分过滤条件的样本' : '暂无样本数据'" />
      </template>
    </el-table>

    <div class="table-pager">
      <el-pagination
        background
        layout="total, sizes, prev, pager, next"
        :total="report.samples.total"
        :current-page="report.samples.page"
        :page-size="report.samples.page_size"
        :page-sizes="[20, 50, 100]"
        @current-change="onPageChange"
        @size-change="onSizeChange"
      />
    </div>
  </div>
</template>

<style scoped>
.filter-chips {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}

.chips-label {
  font-size: 12.5px;
}

.chip {
  font-size: 12.5px;
  transition:
    background-color var(--br-transition-fast),
    color var(--br-transition-fast);
}

.chips-hint {
  font-size: 12px;
}

.eval-table {
  border-radius: var(--br-radius-md);
}

.cell-idx {
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
  color: var(--br-text-tertiary);
  font-size: 12.5px;
}

.cell-text {
  font-size: 13px;
}

.cell-clamp {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.cell-score {
  display: inline-block;
  padding: 1px 8px;
  border-radius: var(--br-radius-sm);
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 12.5px;
  font-weight: 600;
}

.cell-warn {
  color: var(--el-color-warning);
}

.table-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}
</style>
