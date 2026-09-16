// 后端实体类型定义（字段名与后端 JSON 序列化逐一对齐）
// 注意：store 层实体无 json tag，序列化为 PascalCase 字段名（如 ID/KBID/Filename）

// 统一响应包装
export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

// 检索策略配置（三级覆盖：全局 → 知识库 → 单次请求；字段空 = 继承低层级）
export interface StrategyConfig {
  query?: 'single' | 'multi'
  fusion?: 'rrf' | 'none'
  decomposition?: 'off' | 'parallel' | 'sequential'
  step_back?: 'off' | 'on'
  hyde?: 'off' | 'on'
  routing?: 'off' | 'auto'
  thinking?: 'off' | 'on' // 思考链路开关
}

// 知识库
export interface Kb {
  ID: string
  Name: string
  Description: string
  Strategy: string // 策略配置 JSON 字符串（空 = 用全局默认）
  CreatedAt: string
  UpdatedAt: string
}

// 文档（状态字段同时反映入库任务状态）
export type DocStatus = 'pending' | 'processing' | 'completed' | 'failed'

export interface Document {
  ID: string
  KBID: string
  Filename: string
  Format: string
  Size: number
  Status: DocStatus
  ChunkIDs: string[]
  FilePath: string
  TaskID: string
  CreatedAt: string
}

// 入库任务
export interface Task {
  ID: string
  KBID: string
  DocumentID: string
  Status: DocStatus
  RetryCount: number
  ErrorMessage: string
  CreatedAt: string
  UpdatedAt: string
}

// API Key（列表视图，不含 hash；handler 层 keyView 为 snake_case json tag）
export interface ApiKeyView {
  id: string
  name: string
  enabled: boolean
  last_used_at: string | null
  created_at: string
  // MCP 权限（spec F1：空 = 未配置，由后端返回空数组/空字符串）
  mcp_tools: string[]
  mcp_kb_scope: string // '' | 'all' | 'allowlist'
  mcp_kb_ids: string[]
}

// 创建 API Key 响应（明文仅此一次）
export interface CreateKeyResult {
  ID: string
  Name: string
  key: string
}

// 上传文档响应
export interface UploadResult {
  task_id: string
  document_id: string
}

// 支持的文件类型（后端 /documents/supported-types 返回，与 loader.SupportedType 对齐）
export interface SupportedType {
  ext: string
  category: 'text' | 'image' | 'audio' | 'video'
  supported: boolean
  reason?: string
}

// 对话
// ---- 思考链路（thinking）----
// 环节类型与后端 ThinkingStepType 对齐
// 各环节 Data 载荷（与后端 json tag 对齐）

export interface RoutingData {
  complexity: 'simple' | 'medium' | 'complex'
  strategy: string
  reasoning?: string
}

export interface RewriteData {
  original: string
  rewritten: string
  fallback?: boolean
}

export interface MultiQueryData {
  variants: string[]
}

export interface PerQueryRet {
  query: string
  method?: string
  recalled: number
}

export interface RetrievalData {
  query: string
  per_query?: PerQueryRet[]
  method?: string
  recalled: number
}

export interface RankedItem {
  id: string
  filename: string
  score: number
  rank: number
}

export interface RerankData {
  query: string
  before: RankedItem[]
  after: RankedItem[]
}

export interface ChunkInfo {
  id: string
  filename: string
  heading: string
  score: number
  content: string
}

export interface ChunksData {
  chunks: ChunkInfo[]
}

export interface DecomposeData {
  should_decompose: boolean
  sub_questions?: string[]
}

export interface StepBackData {
  step_back_query: string
}

export interface HyDEData {
  hypo_doc: string
}

// 工具调用（增强模式 function calling）
export interface ToolStepItem {
  title: string
  url?: string
  snippet?: string
}

export interface ToolStepData {
  name: string
  args?: string
  result?: string
  items?: ToolStepItem[]
  error?: string
}

export interface ThinkingStep {
  type: string
  label: string
  elapsed_ms?: number
  data?: any
}

export interface ChatSource {
  id: string
  filename: string
  heading: string
  score: number
  source_type?: string
  start_ms?: number
  end_ms?: number
  page_number?: number
  anchor?: string
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
}

export interface ChatRequest {
  session_id: string
  question: string
  kb_id: string // 知识库范围，空表示不限定
  strategy?: StrategyConfig // 单次请求策略覆盖
  enhanced?: boolean // 增强模式（联网搜索等 function calling 工具）
}

// SSE 事件（后端序列：thinking×N → sources → chunk×N → done，或 error）
export type SSEEvent =
  | { type: 'thinking'; step: ThinkingStep }
  | { type: 'sources'; sources: ChatSource[] }
  | { type: 'chunk'; content: string }
  | { type: 'done' }
  | { type: 'error'; message: string }

// 会话索引（前端本地 localStorage 持久化）
export interface SessionMeta {
  id: string
  title: string
  kbId: string
  updatedAt: string
}

// 配置视图（后端 handler_config）
export interface ConfigView {
  mutable: {
    llm: { model: string; temperature: number; max_tokens: number; timeout: number }
    embedder: { model: string; dimension: number }
    retriever: { top_k: number; rrf_k: number; vector_weight: number; bm25_weight: number }
    rag_strategy: StrategyConfig
    loader: { min_readable_chars: number }
    mcp: { enabled: boolean; path: string; audit_param_limit: number }
  }
  read_only: Array<{ key: string; value: string; needs_restart: boolean }>
  is_bootstrap: boolean // 当前 Key 是否为 bootstrap（后端权威判断）
}

// —— 我的 MCP（用户维度凭据，spec F7）——
export interface MyMCPKey {
  id: string
  enabled: boolean
  mcp_tools: string[]
  mcp_kb_scope: string // '' | 'all' | 'allowlist'
  mcp_kb_ids: string[]
}

export interface MyMCPStatus {
  global_enabled: boolean
  key: MyMCPKey | null
  mcp_path: string
}

export interface CreateMyKeyResult {
  id: string
  key: string // 明文仅此一次
}

// ==========================================================================
// RAGAS 评测（docs/35-ragas评测）
// 本模块对接 Python/FastAPI 评测微服务（经 Go 反向代理透传），端点与字段
// **全 snake_case**（与 ApiKeyView 同样的先例）。类型以 architect-design §2
// 契约为准：与 frontend-design §3.3 草案冲突处（状态枚举、progress 字段、
// 分页包装、JudgeModel 字段、报告结构）均已按 architect-design 修正。
// ==========================================================================

// 通用分页包装（列表响应 data 形态）
export interface Paged<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

// 四指标
export type EvalMetric =
  | 'faithfulness'
  | 'answer_relevancy'
  | 'context_precision'
  | 'context_recall'

// 任务状态机（architect-design §4.1）：pending→collecting→evaluating→completed；运行态→canceled/failed
// 注：frontend-design 草案中的 running/cancelled 已按契约改为 collecting/evaluating/canceled
export type EvalTaskStatus =
  | 'pending'
  | 'collecting'
  | 'evaluating'
  | 'completed'
  | 'failed'
  | 'canceled'

// —— 数据集（architect-design §2.3-2/3/4）——
export interface EvalDataset {
  id: string
  name: string
  sample_count: number
  with_reference_count: number // 含标准答案的样本数（为 0 时禁用 context_recall）
  source_format: string // json | jsonl
  content_hash: string
  created_at: string
}

// 上传响应：DatasetView + 校验警告
export interface EvalDatasetUploadResult extends EvalDataset {
  validation?: { warnings: string[] }
}

// 数据集预览（§2.3-4）
export interface EvalDatasetPreview {
  dataset: EvalDataset
  field_stats: { with_reference: number; with_expected_ids: number; with_kb_id: number }
  samples: Array<{ question: string; answer?: string; expected_ids?: string[]; kb_id?: string }>
}

// 评审模型（§2.3-6）：清单由服务端配置驱动，字段为 id/label（非草案的 name/provider）
export interface JudgeModel {
  id: string
  label: string
  is_default: boolean
  supports_structured_output: boolean
}

// 提交评测任务（§2.3-7 请求体）
// 注：frontend-design 草案的 options.{sample_limit,timeout_s,seed} 不在契约内，已移除；
// 高级参数仅保留契约中的 sample_concurrency 与 strategy（透传 Go chat 策略覆盖）
export interface CreateEvalTaskPayload {
  name?: string // 空则由后端自动生成
  dataset_id: string
  kb_id?: string // 可选；为空时逐样本用其自带 kb_id
  judge_model?: string // 可选，默认服务端 default
  metrics?: EvalMetric[] // 可选，默认全量
  sample_concurrency?: number // 样本级并发上限（默认 4，上限 8）
  strategy?: StrategyConfig | null // 可选，透传 chat 的 strategy 覆盖
}

// 样本粒度进度（§2.3-9）
export interface EvalProgress {
  total: number
  collected: number // 采集完成数
  evaluated: number // 评测完成数
  failed: number // 失败样本数（不阻断整体）
}

// 任务（列表视图与详情共用骨架；详情为轮询主接口，必须轻量、不含样本明细）
export interface EvalTask {
  id: string
  name: string
  status: EvalTaskStatus
  stage?: string // 展示用当前阶段（与 status 同义，预留子阶段）
  dataset_id: string
  kb_id?: string
  judge_model: string
  metrics: EvalMetric[]
  progress: EvalProgress
  error_message?: string
  created_at: string
  started_at?: string | null
  finished_at?: string | null
  report_ready?: boolean
  // 注：契约未明确列表是否带汇总均值；若后端提供则用于任务卡迷你指标，缺省则前端不展示
  summary?: Partial<Record<EvalMetric, number>>
}

// 汇总报告单指标统计（§2.3-12）
export interface EvalMetricSummary {
  mean: number | null // 无有效样本时为 null
  coverage: number // 有效样本覆盖率 0~1
  valid_samples: number
  degraded?: boolean // context_precision 使用无参考变体时标注降级口径
  note?: string // 如「16 条样本缺 reference，记 N/A」
}

// 可复现与公平对比的配置快照（§2.3-12）
export interface EvalConfigSnapshot {
  dataset_hash: string
  judge_model: string
  binrag_config_hash?: string
  ragas_version?: string
  prompt_version: string
}

// 报告明细行（不含 contexts 正文，下钻接口才给）
export interface EvalSampleRow {
  sample_id: string
  question: string
  answer_excerpt: string
  scores: Partial<Record<EvalMetric, number | null>>
  status: string // ok | error（单样本失败不阻断整体）
  error?: string
}

// 汇总报告 + 明细分页（§2.3-12；任务未完成 409）
export interface EvalReport {
  task: EvalTask
  summary: Partial<Record<EvalMetric, EvalMetricSummary>>
  config_snapshot: EvalConfigSnapshot
  samples: Paged<EvalSampleRow>
  // 注：契约未含逐样本分数分布；若后端后续补充则直接用，否则前端由样本批次自行分桶
  distribution?: Partial<Record<EvalMetric, number[]>>
}

// 单样本下钻的检索片段（§2.3-13）
export interface EvalContext {
  id: string
  filename?: string
  heading?: string
  score?: number
  content: string
}

// 下钻分数值：纯分数，或附带 judge 原始理由
export type EvalScoreValue = number | { score: number; rationale?: string } | null

// 单样本完整内容（§2.3-13）
export interface EvalSampleDetail {
  sample_id: string
  question: string
  reference?: string
  answer: string
  contexts: EvalContext[]
  scores: Partial<Record<EvalMetric, EvalScoreValue>>
  status: string
  error?: string
}

// 批量汇总项（§2.3-14，对比视图用）
export interface EvalCompareItem {
  task_id: string
  name: string
  finished_at?: string | null
  summary: Partial<Record<EvalMetric, EvalMetricSummary>>
  config_snapshot: EvalConfigSnapshot
}

// 查询参数（§2.3-8/12）
export interface EvalTaskListQuery {
  status?: EvalTaskStatus
  kb_id?: string
  page?: number
  page_size?: number
}

export interface EvalReportQuery {
  page?: number
  page_size?: number
  sort?: string // 形如 "faithfulness:asc"（契约格式，非草案的 sort_by/sort_order）
  metric_lt?: string // 低分过滤，形如 "faithfulness:0.6"
}
