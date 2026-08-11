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
