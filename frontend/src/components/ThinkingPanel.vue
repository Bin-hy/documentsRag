<script setup lang="ts">
// 思考链路面板：时间线化展示 RAG 各环节（路由/改写/多查询/检索/rerank/目标片段/策略环节）
// 数据来自后端 thinking SSE 事件（分步累积）。active 表示流式进行中，最后一步视为"当前环节"。
import { computed, ref } from 'vue'
import type {
  ChunksData,
  DecomposeData,
  HyDEData,
  MultiQueryData,
  RerankData,
  RetrievalData,
  RewriteData,
  RoutingData,
  StepBackData,
  ThinkingStep,
  ToolStepData,
} from '../api/types'

const props = defineProps<{ steps: ThinkingStep[]; active?: boolean }>()

const expanded = ref(false)

// 每个 chunk 的展开状态（按 index 记录）
const expandedChunks = ref<Record<number, boolean>>({})

// 环节显示标题：优先用后端 label，兜底按 type 映射
const typeLabels: Record<string, string> = {
  routing: '路由判定',
  query_rewrite: '查询改写',
  multi_query: '多查询改写',
  retrieval: '检索',
  rerank: '重排序',
  chunks: '目标片段',
  decompose: '问题分解',
  step_back: '回退查询',
  hyde: 'HyDE 假设文档',
  tool: '工具调用',
}

function labelOf(step: ThinkingStep): string {
  return step.label || typeLabels[step.type] || step.type
}

function is(data: unknown, t: string): boolean {
  return data !== null && typeof data === 'object' && typeof (data as Record<string, unknown>)[t] !== 'undefined'
}

function as<T>(data: unknown): T {
  return data as T
}

function formatScore(score: number | undefined): string {
  return score === undefined ? '—' : score.toFixed(3)
}

function formatMs(ms: number | undefined): string {
  return ms === undefined || ms < 0 ? '—' : `${ms}ms`
}

const totalElapsed = computed(() => props.steps.reduce((sum, s) => sum + (s.elapsed_ms ?? 0), 0))

// 当前环节名：流式进行中取最后一步
const currentLabel = computed(() => {
  if (!props.active || props.steps.length === 0) return ''
  return labelOf(props.steps[props.steps.length - 1])
})

// 步骤状态：进行中（流式中且为最后一步）/ 完成 / 等待
function stepState(i: number): 'active' | 'done' {
  return props.active && i === props.steps.length - 1 ? 'active' : 'done'
}

function toggleChunk(i: number) {
  expandedChunks.value[i] = !expandedChunks.value[i]
}
</script>

<template>
  <div v-if="steps.length" class="thinking-wrap" :class="{ open: expanded }">
    <!-- 折叠头 -->
    <div class="thinking-header" role="button" tabindex="0" @click="expanded = !expanded" @keydown.enter="expanded = !expanded" @keydown.space.prevent="expanded = !expanded">
      <span class="status-orb" :class="{ pulsing: active }" :title="active ? '处理中' : '已完成'" />
      <span class="thinking-title">思考过程</span>
      <span v-if="currentLabel" class="current-step">{{ currentLabel }}</span>
      <span v-else class="current-step done">完成</span>
      <span class="thinking-count">{{ steps.length }} 环节</span>
      <span v-if="totalElapsed" class="thinking-elapsed">{{ totalElapsed }}ms</span>
      <span class="thinking-chevron" :class="{ open: expanded }">{{ expanded ? '▾' : '▸' }}</span>
    </div>

    <!-- 展开后的环节时间线（v-show 条件显示，保证展开必有内容） -->
    <div v-show="expanded" class="thinking-body">
      <div class="timeline">
          <div
            v-for="(step, i) in steps"
            :key="i"
            class="thinking-step"
            :class="stepState(i)"
          >
              <div class="step-head">
                <span class="step-node" />
                <span class="step-label">{{ labelOf(step) }}</span>
                <span v-if="step.elapsed_ms" class="step-elapsed">{{ formatMs(step.elapsed_ms) }}</span>
              </div>

              <!-- 路由判定 -->
              <div v-if="step.type === 'routing'" class="step-data">
                <template v-if="is(step.data, 'complexity')">
                  <div class="kv"><span class="k">复杂度</span><span class="v">{{ as<RoutingData>(step.data).complexity }}</span></div>
                  <div class="kv"><span class="k">策略</span><span class="v">{{ as<RoutingData>(step.data).strategy }}</span></div>
                  <div v-if="as<RoutingData>(step.data).reasoning" class="reasoning">{{ as<RoutingData>(step.data).reasoning }}</div>
                </template>
              </div>

              <!-- 单查询改写 -->
              <div v-else-if="step.type === 'query_rewrite'" class="step-data">
                <template v-if="is(step.data, 'rewritten')">
                  <div class="kv">
                    <span class="k">原问题</span>
                    <span class="v">{{ as<RewriteData>(step.data).original }}</span>
                  </div>
                  <div v-if="as<RewriteData>(step.data).fallback" class="reasoning">改写失败，使用原问题</div>
                  <div v-else class="kv">
                    <span class="k">改写后</span>
                    <span class="v">{{ as<RewriteData>(step.data).rewritten }}</span>
                  </div>
                </template>
              </div>

              <!-- 多查询改写 -->
              <div v-else-if="step.type === 'multi_query'" class="step-data">
                <ul v-if="is(step.data, 'variants')" class="variant-list">
                  <li v-for="(v, vi) in as<MultiQueryData>(step.data).variants" :key="vi">{{ v }}</li>
                </ul>
              </div>

              <!-- 检索 -->
              <div v-else-if="step.type === 'retrieval'" class="step-data">
                <template v-if="is(step.data, 'recalled')">
                  <div v-if="(as<RetrievalData>(step.data).per_query || []).length" class="per-query">
                    <div v-for="(pq, pi) in as<RetrievalData>(step.data).per_query" :key="pi" class="kv">
                      <span class="k">路 {{ pi + 1 }}</span>
                      <span class="v">{{ pq.query }}（{{ pq.method || '检索' }}，召回 {{ pq.recalled }}）</span>
                    </div>
                    <div class="kv"><span class="k">融合</span><span class="v">召回 {{ as<RetrievalData>(step.data).recalled }} 条</span></div>
                  </div>
                  <div v-else class="kv">
                    <span class="k">查询</span>
                    <span class="v">{{ as<RetrievalData>(step.data).query }}</span>
                  </div>
                  <div v-if="as<RetrievalData>(step.data).method && !(as<RetrievalData>(step.data).per_query || []).length" class="kv">
                    <span class="k">方式</span>
                    <span class="v">{{ as<RetrievalData>(step.data).method }}，召回 {{ as<RetrievalData>(step.data).recalled }} 条</span>
                  </div>
                </template>
              </div>

              <!-- rerank 前后对比 -->
              <div v-else-if="step.type === 'rerank'" class="step-data">
                <template v-if="is(step.data, 'before')">
                  <div class="rerank-cols">
                    <div class="rerank-col">
                      <div class="col-title">重排前</div>
                      <div v-for="(it, ri) in as<RerankData>(step.data).before" :key="ri" class="rank-item">
                        <span class="rank-no">#{{ it.rank }}</span>
                        <span class="rank-name">{{ it.filename }}</span>
                        <span class="rank-score">{{ formatScore(it.score) }}</span>
                      </div>
                    </div>
                    <div class="rerank-col">
                      <div class="col-title">重排后</div>
                      <div v-for="(it, ri) in as<RerankData>(step.data).after" :key="ri" class="rank-item">
                        <span class="rank-no">#{{ it.rank }}</span>
                        <span class="rank-name">{{ it.filename }}</span>
                        <span class="rank-score">{{ formatScore(it.score) }}</span>
                      </div>
                    </div>
                  </div>
                </template>
              </div>

              <!-- 目标片段 -->
              <div v-else-if="step.type === 'chunks'" class="step-data">
                <template v-if="is(step.data, 'chunks')">
                  <div v-for="(c, ci) in as<ChunksData>(step.data).chunks" :key="c.id" class="chunk-item">
                    <div class="kv">
                      <span class="k">#{{ ci + 1 }}</span>
                      <span class="v">{{ c.filename }}{{ c.heading ? ' / ' + c.heading : '' }}（{{ formatScore(c.score) }}）</span>
                    </div>
                    <div class="chunk-content" :class="{ clamped: !expandedChunks[ci] }">{{ c.content }}</div>
                    <button v-if="c.content.length > 120" class="chunk-toggle" @click="toggleChunk(ci)">
                      {{ expandedChunks[ci] ? '收起' : '展开' }}
                    </button>
                  </div>
                </template>
              </div>

              <!-- 问题分解 -->
              <div v-else-if="step.type === 'decompose'" class="step-data">
                <template v-if="is(step.data, 'sub_questions')">
                  <ul class="variant-list">
                    <li v-for="(s, si) in as<DecomposeData>(step.data).sub_questions || []" :key="si">{{ s }}</li>
                  </ul>
                </template>
              </div>

              <!-- 回退查询 -->
              <div v-else-if="step.type === 'step_back'" class="step-data">
                <template v-if="is(step.data, 'step_back_query')">
                  <div class="kv"><span class="k">回退问题</span><span class="v">{{ as<StepBackData>(step.data).step_back_query }}</span></div>
                </template>
              </div>

              <!-- HyDE 假设文档 -->
              <div v-else-if="step.type === 'hyde'" class="step-data">
                <template v-if="is(step.data, 'hypo_doc')">
                  <div class="hypo-doc">{{ as<HyDEData>(step.data).hypo_doc }}</div>
                </template>
              </div>

              <!-- 工具调用（增强模式 function calling） -->
              <div v-else-if="step.type === 'tool'" class="step-data">
                <template v-if="is(step.data, 'name')">
                  <div class="kv"><span class="k">工具</span><span class="v">{{ as<ToolStepData>(step.data).name }}</span></div>
                  <div v-if="as<ToolStepData>(step.data).args" class="kv">
                    <span class="k">参数</span><span class="v">{{ as<ToolStepData>(step.data).args }}</span>
                  </div>
                  <div v-if="(as<ToolStepData>(step.data).items || []).length" class="kv">
                    <span class="k">结果</span>
                    <div class="tool-items">
                      <div v-for="(item, ii) in as<ToolStepData>(step.data).items" :key="ii" class="tool-item">
                        <a v-if="item.url" :href="item.url" target="_blank" rel="noopener" class="tool-item-title">{{ item.title }}</a>
                        <span v-else class="tool-item-title">{{ item.title }}</span>
                        <div v-if="item.snippet" class="tool-item-snippet">{{ item.snippet }}</div>
                      </div>
                    </div>
                  </div>
                  <div v-else-if="as<ToolStepData>(step.data).result" class="kv">
                    <span class="k">结果</span><span class="v">{{ as<ToolStepData>(step.data).result }}</span>
                  </div>
                  <div v-if="as<ToolStepData>(step.data).error" class="kv">
                    <span class="k">错误</span><span class="v">{{ as<ToolStepData>(step.data).error }}</span>
                  </div>
                </template>
              </div>
            </div>
        </div>
    </div>
  </div>
</template>

<style scoped>
.thinking-wrap {
  margin-bottom: 12px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-md);
  background: var(--br-bg-inset);
  font-size: 13px;
  overflow: hidden;
  transition:
    border-color var(--br-transition-fast),
    box-shadow var(--br-transition-fast);
}

.thinking-wrap:hover {
  border-color: var(--br-border-strong);
}

.thinking-wrap.open {
  box-shadow: var(--br-shadow-sm);
}

/* ---------- 折叠头 ---------- */
.thinking-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 13px;
  cursor: pointer;
  user-select: none;
  color: var(--br-text-secondary);
  transition: background-color var(--br-transition-fast);
}

.thinking-header:hover {
  background: var(--br-bg-hover);
}

.thinking-header:focus-visible {
  outline: 2px solid var(--br-primary);
  outline-offset: -2px;
}

/* 状态圆点：进行中脉冲 / 完成 */
.status-orb {
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--br-primary);
  box-shadow: 0 0 0 3px var(--br-primary-soft);
}

.status-orb.pulsing {
  animation: orb-pulse 1.4s var(--br-ease) infinite;
}

@keyframes orb-pulse {
  0% {
    box-shadow: 0 0 0 0 var(--br-primary-soft-2);
  }
  70% {
    box-shadow: 0 0 0 7px transparent;
  }
  100% {
    box-shadow: 0 0 0 0 transparent;
  }
}

.thinking-title {
  color: var(--br-text);
  font-weight: 600;
}

.current-step {
  font-size: 12px;
  color: var(--br-primary);
}

.current-step.done {
  color: var(--br-text-tertiary);
}

.thinking-count {
  color: var(--br-text-tertiary);
  font-size: 11.5px;
  background: var(--br-bg-hover);
  border: 1px solid var(--br-border);
  padding: 1px 8px;
  border-radius: var(--br-radius-pill);
  font-variant-numeric: tabular-nums;
}

.thinking-elapsed {
  color: var(--br-text-tertiary);
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
}

.thinking-chevron {
  margin-left: auto;
  font-size: 11px;
  color: var(--br-text-tertiary);
  transition: transform var(--br-transition-base);
}

.thinking-chevron.open {
  transform: rotate(180deg);
}

/* ---------- 展开区（v-show 条件显示，展开必有内容） ---------- */
.thinking-body {
  border-top: 1px solid var(--br-border);
}

.timeline {
  position: relative;
  margin: 0 6px;
  padding: 12px 0 12px 22px;
}

/* 垂直时间线 */
.timeline::before {
  content: '';
  position: absolute;
  left: 6px;
  top: 16px;
  bottom: 14px;
  width: 2px;
  border-radius: 1px;
  background: var(--br-border);
}

.thinking-step {
  position: relative;
  padding-bottom: 12px;
  /* 入场动画：无 fill-mode，即使动画不播放元素也保持可见（opacity 1） */
  animation: step-in 320ms var(--br-ease);
}

.thinking-step:last-child {
  padding-bottom: 0;
}

@keyframes step-in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

/* 步骤节点 */
.step-node {
  position: absolute;
  left: -20px;
  top: 4px;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--br-primary);
  box-shadow: 0 0 0 3px var(--br-primary-soft);
  border: 2px solid var(--br-bg-inset);
}

.thinking-step.active .step-node {
  animation: node-pulse 1.2s var(--br-ease) infinite;
}

@keyframes node-pulse {
  0% {
    box-shadow: 0 0 0 0 var(--br-primary-soft-2);
  }
  70% {
    box-shadow: 0 0 0 6px transparent;
  }
  100% {
    box-shadow: 0 0 0 0 transparent;
  }
}

.step-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.step-label {
  font-weight: 600;
  color: var(--br-text);
}

.thinking-step.active .step-label {
  color: var(--br-primary);
}

.step-elapsed {
  margin-left: auto;
  color: var(--br-text-tertiary);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}



/* ---------- 数据区 ---------- */
.step-data {
  color: var(--br-text);
  line-height: 1.6;
}

.kv {
  display: flex;
  gap: 8px;
  margin-bottom: 2px;
}

.k {
  flex-shrink: 0;
  color: var(--br-text-secondary);
  font-size: 12px;
  background: var(--br-bg-hover);
  border: 1px solid var(--br-border);
  padding: 0 6px;
  border-radius: 5px;
  line-height: 1.6;
  align-self: flex-start;
}

.v {
  word-break: break-word;
}

.reasoning {
  margin-top: 2px;
  color: var(--br-text-secondary);
  font-style: italic;
}

.variant-list {
  margin: 0;
  padding-left: 18px;
}

.per-query {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

/* rerank 对比：并排卡片 */
.rerank-cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  margin-top: 4px;
}

.rerank-col {
  min-width: 0;
  padding: 8px 10px;
  border-radius: var(--br-radius-sm);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
}

.col-title {
  color: var(--br-text-secondary);
  font-size: 11.5px;
  font-weight: 600;
  margin-bottom: 4px;
}

.rank-item {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 2px;
}

.rank-no {
  color: var(--br-text-tertiary);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.rank-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.rank-score {
  color: var(--br-text-tertiary);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

/* 目标片段卡片 */
.chunk-item {
  margin-bottom: 8px;
  padding: 8px 10px;
  border-radius: var(--br-radius-sm);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
}

.chunk-item:last-child {
  margin-bottom: 0;
}

.chunk-content {
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
}

.chunk-content.clamped {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.chunk-toggle {
  margin-top: 2px;
  border: none;
  background: none;
  color: var(--br-primary);
  cursor: pointer;
  font-size: 12px;
  padding: 0;
}

.hypo-doc {
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--br-text-secondary);
}

.tool-items {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.tool-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.tool-item-title {
  color: var(--br-primary);
  text-decoration: none;
  word-break: break-all;
}

.tool-item-title:hover {
  text-decoration: underline;
}

.tool-item-snippet {
  font-size: 12px;
  color: var(--br-text-secondary);
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
