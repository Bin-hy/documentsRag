// API Key 管理 REST API
import { request } from './client'
import type { ApiKeyView, CreateKeyResult } from './types'

export function createKey(name: string): Promise<CreateKeyResult> {
  return request<CreateKeyResult>({
    method: 'POST',
    url: '/api/v1/api-keys',
    data: { name },
  })
}

export function listKeys(): Promise<ApiKeyView[]> {
  return request<ApiKeyView[]>({ method: 'GET', url: '/api/v1/api-keys' })
}

export function toggleKey(id: string, enabled: boolean): Promise<unknown> {
  return request<unknown>({
    method: 'POST',
    url: `/api/v1/api-keys/${id}/toggle`,
    data: { enabled },
  })
}

export function deleteKey(id: string): Promise<unknown> {
  return request<unknown>({ method: 'DELETE', url: `/api/v1/api-keys/${id}` })
}
