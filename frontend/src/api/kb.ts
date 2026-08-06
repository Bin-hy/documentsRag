// 知识库 REST API
import { request } from './client'
import type { Kb, StrategyConfig } from './types'

export function listKbs(): Promise<Kb[]> {
  return request<Kb[]>({ method: 'GET', url: '/api/v1/knowledge-bases' })
}

export function createKb(name: string, description: string, strategy?: StrategyConfig): Promise<Kb> {
  return request<Kb>({
    method: 'POST',
    url: '/api/v1/knowledge-bases',
    data: { name, description, strategy },
  })
}

export function updateKb(id: string, name: string, description: string, strategy?: StrategyConfig): Promise<Kb> {
  return request<Kb>({
    method: 'PUT',
    url: `/api/v1/knowledge-bases/${id}`,
    data: { name, description, strategy },
  })
}

export function deleteKb(id: string): Promise<unknown> {
  return request<unknown>({ method: 'DELETE', url: `/api/v1/knowledge-bases/${id}` })
}
