// ECharts option 工厂：所有颜色读取 --br-* / --el-* CSS 变量（frontend-design §2.4 主题对接）
// 不加载 ECharts 内置 dark 主题；html.dark 切换时由 EvalChart.vue 触发重建 option。
import type { EChartsOption, SeriesOption } from 'echarts'
import type { EvalMetric } from '../api/types'
import { METRIC_META } from './evalScore'

/** 读取 CSS 变量（带兜底，防 SSR/测试环境无 DOM） */
export function cssVar(name: string, fallback: string): string {
  if (typeof getComputedStyle !== 'function') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

// 对比视图叠加色板：品牌色 + 绿/橙/紫一组协调色，亮暗两套（frontend-design §2.4）
export const COMPARE_PALETTE = {
  light: ['#4f5bd5', '#3a9e6e', '#e8930c', '#8a5cf6'],
  dark: ['#7d85ee', '#4cc38a', '#f0a53c', '#a98df5'],
} as const

export function comparePalette(isDark: boolean): string[] {
  return [...(isDark ? COMPARE_PALETTE.dark : COMPARE_PALETTE.light)]
}

function themeColors() {
  return {
    primary: cssVar('--br-primary', '#4f5bd5'),
    text: cssVar('--br-text', '#1b1e2b'),
    textSecondary: cssVar('--br-text-secondary', '#69707f'),
    textTertiary: cssVar('--br-text-tertiary', '#9aa0b0'),
    border: cssVar('--br-border', '#e4e7f0'),
    bgCard: cssVar('--br-bg-card', '#ffffff'),
    bgInset: cssVar('--br-bg-inset', '#eef0f7'),
    success: cssVar('--el-color-success', '#3a9e6e'),
    warning: cssVar('--el-color-warning', '#e8930c'),
  }
}

const MONO = "'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, Consolas, monospace"

function baseTooltip(c: ReturnType<typeof themeColors>) {
  return {
    backgroundColor: c.bgCard,
    borderColor: c.border,
    textStyle: { color: c.text, fontSize: 12.5 },
  }
}

interface RadarSeriesInput {
  name: string
  // 与 metrics 对齐的均值；null（无有效样本）按 0 绘制
  values: Array<number | null>
}

/** 单任务/多任务叠加雷达图（metrics 为维度顺序） */
export function buildRadarOption(
  metrics: EvalMetric[],
  series: RadarSeriesInput[],
  isDark: boolean,
): EChartsOption {
  const c = themeColors()
  const palette = comparePalette(isDark)
  return {
    color: series.length > 1 ? palette : [c.primary],
    tooltip: { ...baseTooltip(c), trigger: 'item' },
    legend:
      series.length > 1
        ? { bottom: 0, textStyle: { color: c.textSecondary, fontSize: 12 }, icon: 'roundRect' }
        : undefined,
    radar: {
      indicator: metrics.map((m) => ({ name: METRIC_META[m].label, max: 1 })),
      radius: '62%',
      center: ['50%', series.length > 1 ? '46%' : '50%'],
      axisName: { color: c.textSecondary, fontSize: 12.5 },
      splitLine: { lineStyle: { color: c.border } },
      splitArea: { areaStyle: { color: [c.bgCard, c.bgInset] } },
      axisLine: { lineStyle: { color: c.border } },
    },
    series: [
      {
        type: 'radar',
        symbolSize: 4,
        data: series.map((s, i) => ({
          name: s.name,
          value: s.values.map((v) => (v == null || Number.isNaN(v) ? 0 : v)),
          lineStyle: { width: 2 },
          areaStyle:
            series.length === 1
              ? { color: palette[0], opacity: 0.18 }
              : { color: palette[i % palette.length], opacity: 0.08 },
        })),
      },
    ],
  }
}

// 分布直方图分桶（0~1 五桶）
const DIST_BUCKETS = ['0.0~0.2', '0.2~0.4', '0.4~0.6', '0.6~0.8', '0.8~1.0'] as const

export function bucketize(scores: number[]): number[] {
  const bins = [0, 0, 0, 0, 0]
  for (const s of scores) {
    if (Number.isNaN(s)) continue
    const i = Math.min(4, Math.max(0, Math.floor(s * 5)))
    bins[i]++
  }
  return bins
}

/** 逐指标分数分布：X=分数区间桶，每指标一组并列柱（分布比均值更有调优价值） */
export function buildDistributionOption(
  metrics: EvalMetric[],
  distribution: Partial<Record<EvalMetric, number[]>>,
  isDark: boolean,
): EChartsOption {
  const c = themeColors()
  const palette = comparePalette(isDark)
  return {
    color: metrics.map((_, i) => palette[i % palette.length]),
    tooltip: { ...baseTooltip(c), trigger: 'axis' },
    legend: { bottom: 0, textStyle: { color: c.textSecondary, fontSize: 12 }, icon: 'roundRect' },
    grid: { left: 44, right: 16, top: 20, bottom: 52 },
    xAxis: {
      type: 'category',
      data: [...DIST_BUCKETS],
      axisLine: { lineStyle: { color: c.border } },
      axisTick: { show: false },
      axisLabel: { color: c.textTertiary, fontSize: 11.5, fontFamily: MONO },
    },
    yAxis: {
      type: 'value',
      name: '样本数',
      nameTextStyle: { color: c.textTertiary, fontSize: 11 },
      minInterval: 1,
      splitLine: { lineStyle: { color: c.border } },
      axisLabel: { color: c.textTertiary, fontSize: 11.5, fontFamily: MONO },
    },
    series: metrics.map(
      (m): SeriesOption => ({
        type: 'bar',
        name: METRIC_META[m].label,
        data: bucketize(distribution[m] ?? []),
        barMaxWidth: 22,
        itemStyle: { borderRadius: [4, 4, 0, 0] },
      }),
    ),
  }
}

/** 对比分组柱状图：X=四指标，每组内多任务并列柱 */
export function buildCompareBarOption(
  metrics: EvalMetric[],
  series: RadarSeriesInput[],
  isDark: boolean,
): EChartsOption {
  const c = themeColors()
  const palette = comparePalette(isDark)
  return {
    color: series.map((_, i) => palette[i % palette.length]),
    tooltip: { ...baseTooltip(c), trigger: 'axis' },
    legend: { bottom: 0, textStyle: { color: c.textSecondary, fontSize: 12 }, icon: 'roundRect' },
    grid: { left: 44, right: 16, top: 20, bottom: 52 },
    xAxis: {
      type: 'category',
      data: metrics.map((m) => METRIC_META[m].label),
      axisLine: { lineStyle: { color: c.border } },
      axisTick: { show: false },
      axisLabel: { color: c.textSecondary, fontSize: 12 },
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: 1,
      splitLine: { lineStyle: { color: c.border } },
      axisLabel: { color: c.textTertiary, fontSize: 11.5, fontFamily: MONO },
    },
    series: series.map(
      (s): SeriesOption => ({
        type: 'bar',
        name: s.name,
        data: s.values.map((v) => (v == null || Number.isNaN(v) ? null : v)),
        barMaxWidth: 26,
        itemStyle: { borderRadius: [4, 4, 0, 0] },
      }),
    ),
  }
}
