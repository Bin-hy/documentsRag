// RAGAS 评测 REST API（对接 Python 评测微服务，经 Go 反向代理透传）
// 端点契约以 docs/35-ragas评测/architect-design.md §2 为准：全 snake_case、
// 统一 {code,message,data} 包装、列表响应为 {items,total,page,page_size} 分页结构。
// 复用 client.ts 的 request<T>()（Bearer 凭据 / 401 跳登录），不新建 axios 实例。
import { request } from './client'
import type {
  CreateEvalTaskPayload,
  EvalCompareItem,
  EvalDataset,
  EvalDatasetPreview,
  EvalDatasetUploadResult,
  EvalReport,
  EvalReportQuery,
  EvalSampleDetail,
  EvalTask,
  EvalTaskListQuery,
  JudgeModel,
  Paged,
} from './types'

const BASE = '/api/v1/eval'

// —— 数据集 ——

export function listEvalDatasets(): Promise<Paged<EvalDataset>> {
  return request<Paged<EvalDataset>>({ method: 'GET', url: `${BASE}/datasets` })
}

/** 上传数据集（multipart，json/jsonl ≤10MB；对齐 api/doc.ts 的 uploadDocument 写法） */
export function uploadEvalDataset(file: File, name?: string): Promise<EvalDatasetUploadResult> {
  const form = new FormData()
  form.append('file', file)
  if (name) form.append('name', name)
  return request<EvalDatasetUploadResult>({
    method: 'POST',
    url: `${BASE}/datasets`,
    data: form,
    headers: { 'Content-Type': 'multipart/form-data' },
  })
}

export function previewEvalDataset(id: string, limit = 5): Promise<EvalDatasetPreview> {
  return request<EvalDatasetPreview>({
    method: 'GET',
    url: `${BASE}/datasets/${encodeURIComponent(id)}/preview`,
    params: { limit },
  })
}

export function deleteEvalDataset(id: string): Promise<unknown> {
  return request<unknown>({ method: 'DELETE', url: `${BASE}/datasets/${encodeURIComponent(id)}` })
}

// —— 评审模型 ——

export function listJudgeModels(): Promise<{ items: JudgeModel[] }> {
  return request<{ items: JudgeModel[] }>({ method: 'GET', url: `${BASE}/judge-models` })
}

// —— 任务 ——

/** 提交评测任务（幂等：idempotencyKey 由调用方在表单会话内保持稳定，防双击/重试重复提交） */
export function createEvalTask(
  payload: CreateEvalTaskPayload,
  idempotencyKey?: string,
): Promise<EvalTask> {
  return request<EvalTask>({
    method: 'POST',
    url: `${BASE}/tasks`,
    data: payload,
    headers: idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : undefined,
  })
}

export function listEvalTasks(query?: EvalTaskListQuery): Promise<Paged<EvalTask>> {
  return request<Paged<EvalTask>>({ method: 'GET', url: `${BASE}/tasks`, params: query })
}

/** 任务详情/进度（轮询打这个，轻量、不含样本明细） */
export function getEvalTask(id: string): Promise<EvalTask> {
  return request<EvalTask>({ method: 'GET', url: `${BASE}/tasks/${encodeURIComponent(id)}` })
}

export function cancelEvalTask(id: string): Promise<EvalTask> {
  return request<EvalTask>({ method: 'POST', url: `${BASE}/tasks/${encodeURIComponent(id)}/cancel` })
}

export function deleteEvalTask(id: string): Promise<unknown> {
  return request<unknown>({ method: 'DELETE', url: `${BASE}/tasks/${encodeURIComponent(id)}` })
}

// —— 报告 ——

export function getEvalReport(id: string, query?: EvalReportQuery): Promise<EvalReport> {
  return request<EvalReport>({
    method: 'GET',
    url: `${BASE}/tasks/${encodeURIComponent(id)}/report`,
    params: query,
  })
}

/** 单样本完整内容（question/answer/contexts 正文/分数与理由，供下钻抽屉） */
export function getEvalSample(taskId: string, sampleId: string): Promise<EvalSampleDetail> {
  return request<EvalSampleDetail>({
    method: 'GET',
    url: `${BASE}/tasks/${encodeURIComponent(taskId)}/report/samples/${encodeURIComponent(sampleId)}`,
  })
}

/** 多任务汇总批量查询（对比视图，契约上限 8 个） */
export function getEvalReportsBatch(taskIds: string[]): Promise<{ items: EvalCompareItem[] }> {
  return request<{ items: EvalCompareItem[] }>({
    method: 'GET',
    url: `${BASE}/reports`,
    params: { task_ids: taskIds.join(',') },
  })
}
