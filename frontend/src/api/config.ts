// 系统配置 API
import { request } from './client'
import type { ConfigView } from './types'

export function getConfig(): Promise<ConfigView> {
  return request<ConfigView>({ method: 'GET', url: '/api/v1/config' })
}

export function updateConfig(patch: Record<string, unknown>): Promise<ConfigView> {
  return request<ConfigView>({ method: 'PUT', url: '/api/v1/config', data: patch })
}
