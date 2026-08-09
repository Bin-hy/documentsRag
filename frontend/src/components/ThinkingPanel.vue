<script setup lang="ts">
// 思考链路面板：可折叠展示 RAG 各环节（路由/改写/多查询/检索/rerank/目标片段/策略环节）
// 数据来自后端 thinking SSE 事件（分步累积，本组件只做渲染）
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

const props = defineProps<{ steps: ThinkingStep[] }>()

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

const totalElapsed = computed(() => props.steps.reduce((sum, s) => sum + (s.elapsed_ms ?? 0), 0))

function toggleChunk(i: number) {
  expandedChunks.value[i] = !expandedChunks.value[i]
}
</script>

<template>
  <div v-if="steps.length" class="thinking-wrap">
    <!-- 折叠头 -->
    <div class="thinking-header" @click="expanded = !expanded">
      <span class="thinking-toggle">{{ expanded ? '▾' : '▸' }}</span>
      <span class="thinking-title">思考过程</span>
      <span class="thinking-count">{{ steps.length }} 个环节</span>
      <span v-if="totalElapsed" class="thinking-elapsed">{{ totalElapsed }}ms</span>
    </div>

    <!-- 展开后的环节列表 -->
    <div v-if="expanded" class="thinking-body">
      <div v-for="(step, i) in steps" :key="i" class="thinking-step">
        <div class="step-head">
          <span class="step-idx">{{ i + 1 }}</span>
          <span class="step-label">{{ labelOf(step) }}</span>
          <span v-if="step.elapsed_ms" class="step-elapsed">{{ step.elapsed_ms }}ms</span>
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
            <div v-if="as<ToolStepData>(step.data).result" class="kv">
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
</template>

<style scoped>
.thinking-wrap {
  margin-bottom: 12px;
  border: 1px solid var(--br-border);
  border-radius: 10px;
  background: var(--br-bg);
  font-size: 13px;
}

.thinking-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  cursor: pointer;
  user-select: none;
  color: var(--br-text-secondary);
}

.thinking-toggle {
  font-size: 12px;
  width: 12px;
}

.thinking-title {
  color: var(--br-text);
  font-weight: 600;
}

.thinking-count,
.thinking-elapsed {
  color: var(--br-text-secondary);
  font-size: 12px;
}

.thinking-body {
  border-top: 1px solid var(--br-border);
  padding: 8px 12px 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.thinking-step {
  border-left: 2px solid var(--br-primary);
  padding-left: 10px;
}

.step-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.step-idx {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--br-hover);
  color: var(--br-text-secondary);
  font-size: 11px;
}

.step-label {
  font-weight: 600;
  color: var(--br-text);
}

.step-elapsed {
  margin-left: auto;
  color: var(--br-text-secondary);
  font-size: 11px;
}

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

.rerank-cols {
  display: flex;
  gap: 16px;
}

.rerank-col {
  flex: 1;
  min-width: 0;
}

.col-title {
  color: var(--br-text-secondary);
  font-size: 12px;
  margin-bottom: 4px;
}

.rank-item {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 2px;
}

.rank-no {
  color: var(--br-text-secondary);
  font-size: 11px;
  flex-shrink: 0;
}

.rank-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.rank-score {
  color: var(--br-text-secondary);
  font-size: 11px;
  flex-shrink: 0;
}

.chunk-item {
  margin-bottom: 8px;
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
</style>
