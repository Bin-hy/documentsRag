// 后端实体类型定义（字段名与后端 JSON 序列化逐一对齐）
// 注意：store 层实体无 json tag，序列化为 PascalCase 字段名（如 ID/KBID/Filename）

// 统一响应包装
export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

// 知识库
export interface Kb {
  ID: string
  Name: string
  Description: string
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
}

// SSE 事件（后端序列：sources → chunk×N → done，或 error）
export type SSEEvent =
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
