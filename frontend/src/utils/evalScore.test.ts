// evalScore 纯函数单测：色阶阈值、格式化、分数归一化、耗时格式化
import { describe, it, expect } from 'vitest'
import {
  formatDuration,
  formatScore,
  isActiveStatus,
  normalizeScore,
  scoreColor,
  scoreLevel,
} from './evalScore'

describe('scoreLevel 色阶阈值', () => {
  it('≥0.8 为 good（达标）', () => {
    expect(scoreLevel(0.8)).toBe('good')
    expect(scoreLevel(1)).toBe('good')
  })
  it('0.6~0.8 为 mid（品牌色）', () => {
    expect(scoreLevel(0.6)).toBe('mid')
    expect(scoreLevel(0.799)).toBe('mid')
  })
  it('<0.6 为 low（告警）', () => {
    expect(scoreLevel(0.599)).toBe('low')
    expect(scoreLevel(0)).toBe('low')
  })
  it('null/undefined/NaN 为 none', () => {
    expect(scoreLevel(null)).toBe('none')
    expect(scoreLevel(undefined)).toBe('none')
    expect(scoreLevel(NaN)).toBe('none')
  })
})

describe('scoreColor 走 CSS 变量', () => {
  it('各档位返回变量引用而非硬编码颜色', () => {
    expect(scoreColor(0.9)).toBe('var(--el-color-success)')
    expect(scoreColor(0.7)).toBe('var(--br-primary)')
    expect(scoreColor(0.3)).toBe('var(--el-color-warning)')
    expect(scoreColor(null)).toBe('var(--br-text-tertiary)')
  })
})

describe('formatScore', () => {
  it('保留 3 位小数', () => {
    expect(formatScore(0.87654)).toBe('0.877')
    expect(formatScore(1)).toBe('1.000')
  })
  it('无分数显示 —', () => {
    expect(formatScore(null)).toBe('—')
    expect(formatScore(undefined)).toBe('—')
    expect(formatScore(NaN)).toBe('—')
  })
})

describe('normalizeScore 下钻分数归一化', () => {
  it('纯数字', () => {
    expect(normalizeScore(0.83)).toEqual({ score: 0.83 })
  })
  it('带理由对象', () => {
    expect(normalizeScore({ score: 0.5, rationale: '部分忠实' })).toEqual({
      score: 0.5,
      rationale: '部分忠实',
    })
  })
  it('空值与 NaN', () => {
    expect(normalizeScore(null)).toEqual({ score: null })
    expect(normalizeScore(undefined)).toEqual({ score: null })
    expect(normalizeScore(NaN)).toEqual({ score: null })
  })
})

describe('状态判定', () => {
  it('运行态：pending/collecting/evaluating', () => {
    expect(isActiveStatus('pending')).toBe(true)
    expect(isActiveStatus('collecting')).toBe(true)
    expect(isActiveStatus('evaluating')).toBe(true)
    expect(isActiveStatus('completed')).toBe(false)
    expect(isActiveStatus('failed')).toBe(false)
    expect(isActiveStatus('canceled')).toBe(false)
  })
})

describe('formatDuration', () => {
  it('各量级格式化', () => {
    expect(formatDuration(500)).toBe('500ms')
    expect(formatDuration(45_000)).toBe('45s')
    expect(formatDuration(125_000)).toBe('2m 5s')
    expect(formatDuration(3_725_000)).toBe('1h 2m')
  })
  it('空值显示 —', () => {
    expect(formatDuration(null)).toBe('—')
    expect(formatDuration(undefined)).toBe('—')
  })
})
