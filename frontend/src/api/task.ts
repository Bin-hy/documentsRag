// 入库任务 REST API（详情与重试；任务列表随文档列表返回）
import { request } from './client'
import type { Task } from './types'

export function getTask(id: string): Promise<Task> {
  return request<Task>({ method: 'GET', url: `/api/v1/tasks/${id}` })
}

export function retryTask(id: string): Promise<unknown> {
  return request<unknown>({ method: 'POST', url: `/api/v1/tasks/${id}/retry` })
}
