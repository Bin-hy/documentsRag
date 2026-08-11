// 我的 MCP（用户维度自助管理，spec F7/F8）
import { request } from './client'
import type { CreateMyKeyResult, MyMCPStatus } from './types'

export function myMCPStatus(): Promise<MyMCPStatus> {
  return request<MyMCPStatus>({ method: 'GET', url: '/api/v1/mcp/my/status' })
}

export function createMyKey(): Promise<CreateMyKeyResult> {
  return request<CreateMyKeyResult>({ method: 'POST', url: '/api/v1/mcp/my/key' })
}

export function toggleMyKey(enabled: boolean): Promise<unknown> {
  return request<unknown>({ method: 'POST', url: '/api/v1/mcp/my/key/toggle', data: { enabled } })
}

export function deleteMyKey(): Promise<unknown> {
  return request<unknown>({ method: 'DELETE', url: '/api/v1/mcp/my/key' })
}

export function updateMyPermissions(perms: {
  mcp_tools?: string[]
  mcp_kb_scope?: string
  mcp_kb_ids?: string[]
}): Promise<unknown> {
  return request<unknown>({ method: 'PUT', url: '/api/v1/mcp/my/key/permissions', data: perms })
}
