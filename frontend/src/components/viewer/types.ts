// Viewer 统一抽象类型
export type ViewerType = 'pdf' | 'video' | 'audio' | 'markdown'

// 定位信息：由不同 Reader 自行解释
export interface ViewerLocation {
  page?: number // pdf
  startMs?: number // video / audio
  endMs?: number // video / audio
  anchor?: string // markdown
  heading?: string // markdown
}

// 各 Viewer 组件的统一 Props
export interface ViewerProps {
  documentId: string
  filename: string
  location: ViewerLocation
}
