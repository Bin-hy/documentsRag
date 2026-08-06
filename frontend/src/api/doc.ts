// 文档 REST API（文档状态即入库任务状态）
import { request } from './client'
import type { Document, UploadResult } from './types'

export function listDocuments(kbId: string): Promise<Document[]> {
  return request<Document[]>({
    method: 'GET',
    url: '/api/v1/documents',
    params: { kb_id: kbId },
  })
}

export function uploadDocument(kbId: string, file: File): Promise<UploadResult> {
  const form = new FormData()
  form.append('file', file)
  return request<UploadResult>({
    method: 'POST',
    url: '/api/v1/documents/upload',
    params: { kb_id: kbId },
    data: form,
    headers: { 'Content-Type': 'multipart/form-data' },
  })
}

export function deleteDocument(id: string): Promise<unknown> {
  return request<unknown>({ method: 'DELETE', url: `/api/v1/documents/${id}` })
}
