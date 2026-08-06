// Chunk 内容查询（引用来源点击查看原文）
import { request } from './client'

export interface ChunkDetail {
  chunk_id: string
  content: string
  document_id: string
  filename: string
}

export function getChunk(id: string): Promise<ChunkDetail> {
  return request<ChunkDetail>({ method: 'GET', url: `/api/v1/chunks/${encodeURIComponent(id)}` })
}
