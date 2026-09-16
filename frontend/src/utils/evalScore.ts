// 评测分数 → 色阶 / 文案 / 状态映射 纯函数（frontend-design §5.4：色阶规则集中在此，便于测试）
// 颜色一律返回 CSS 变量引用（--el-color-* 已被主题接管、--br-* 为品牌变量），亮暗主题自适应。
import type { EvalMetric, EvalScoreValue, EvalTaskStatus } from '../api/types'

// 达标色阶阈值：≥0.8 绿 / 0.6~0.8 品牌色 / <0.6 橙（frontend-design §2.3 形态 B）
export const SCORE_THRESHOLDS = { good: 0.8, mid: 0.6 } as const

export type ScoreLevel = 'good' | 'mid' | 'low' | 'none'

export function scoreLevel(score: number | null | undefined): ScoreLevel {
  if (score == null || Number.isNaN(score)) return 'none'
  if (score >= SCORE_THRESHOLDS.good) return 'good'
  if (score >= SCORE_THRESHOLDS.mid) return 'mid'
  return 'low'
}

/** 分数对应的前景色（CSS 变量引用） */
export function scoreColor(score: number | null | undefined): string {
  switch (scoreLevel(score)) {
    case 'good':
      return 'var(--el-color-success)'
    case 'mid':
      return 'var(--br-primary)'
    case 'low':
      return 'var(--el-color-warning)'
    default:
      return 'var(--br-text-tertiary)'
  }
}

/** 分数对应的柔和底色（color-mix 由前景色派生，无需新增硬编码色） */
export function scoreSoftBg(score: number | null | undefined): string {
  if (scoreLevel(score) === 'none') return 'transparent'
  return `color-mix(in srgb, ${scoreColor(score)} 12%, transparent)`
}

/** 分数统一展示：保留 3 位小数；无分数（null/NaN）显示 — */
export function formatScore(score: number | null | undefined): string {
  if (score == null || Number.isNaN(score)) return '—'
  return score.toFixed(3)
}

// —— 指标元信息（展示顺序固定）——
export const METRIC_ORDER: EvalMetric[] = [
  'faithfulness',
  'answer_relevancy',
  'context_precision',
  'context_recall',
]

export const METRIC_META: Record<EvalMetric, { label: string; en: string; desc: string }> = {
  faithfulness: { label: '忠实度', en: 'Faithfulness', desc: '回答是否忠于检索内容' },
  answer_relevancy: { label: '答案相关性', en: 'Answer Relevancy', desc: '回答是否切题' },
  context_precision: {
    label: '上下文精确率',
    en: 'Context Precision',
    desc: '检索结果中相关内容的排序质量',
  },
  context_recall: {
    label: '上下文召回率',
    en: 'Context Recall',
    desc: '检索是否覆盖标准答案所需信息（需要标准答案）',
  },
}

// —— 任务状态元信息（el-tag 语义色）——
export const STATUS_META: Record<
  EvalTaskStatus,
  { label: string; tagType: 'info' | 'primary' | 'success' | 'danger' | 'warning' }
> = {
  pending: { label: '排队中', tagType: 'info' },
  collecting: { label: '采集回答中', tagType: 'primary' },
  evaluating: { label: 'RAGAS 评分中', tagType: 'primary' },
  completed: { label: '已完成', tagType: 'success' },
  failed: { label: '失败', tagType: 'danger' },
  canceled: { label: '已取消', tagType: 'warning' },
}

export function isActiveStatus(s: EvalTaskStatus): boolean {
  return s === 'pending' || s === 'collecting' || s === 'evaluating'
}

export function isTerminalStatus(s: EvalTaskStatus): boolean {
  return !isActiveStatus(s)
}

/** 归一化下钻分数值（契约允许纯数字或 {score, rationale} 对象） */
export function normalizeScore(v: EvalScoreValue | undefined): {
  score: number | null
  rationale?: string
} {
  if (v == null) return { score: null }
  if (typeof v === 'number') return { score: Number.isNaN(v) ? null : v }
  return { score: v.score ?? null, rationale: v.rationale }
}

/** 耗时展示（ms → 人类可读），时间显示约定与现有页面一致不引入 dayjs */
export function formatDuration(ms: number | null | undefined): string {
  if (ms == null || ms < 0 || Number.isNaN(ms)) return '—'
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}
