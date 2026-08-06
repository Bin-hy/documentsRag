// HTTP 客户端：统一 BaseURL、API Key 附加与错误处理
import axios from 'axios'
import type { ApiResponse } from './types'

// 本地存储的凭据 Key
export const API_KEY_STORAGE = 'binrag_api_key'

export function getStoredApiKey(): string {
  return localStorage.getItem(API_KEY_STORAGE) ?? ''
}

export function setStoredApiKey(key: string): void {
  localStorage.setItem(API_KEY_STORAGE, key)
}

export function clearStoredApiKey(): void {
  localStorage.removeItem(API_KEY_STORAGE)
}

const client = axios.create({
  baseURL: '/',
  timeout: 60000,
})

// 请求拦截：附加 Authorization: Bearer <key>
client.interceptors.request.use((config) => {
  const key = getStoredApiKey()
  if (key) {
    config.headers = config.headers ?? {}
    config.headers.Authorization = `Bearer ${key}`
  }
  return config
})

// 响应拦截：统一解包 data，401 清除凭据并回登录页
client.interceptors.response.use(
  (resp) => {
    const body = resp.data as ApiResponse<unknown>
    if (body && typeof body === 'object' && 'code' in body && body.code !== 0) {
      return Promise.reject(new Error(body.message || '请求失败'))
    }
    return resp
  },
  (error) => {
    if (error.response?.status === 401) {
      clearStoredApiKey()
      if (!window.location.pathname.startsWith('/login')) {
        window.location.href = '/login'
      }
    }
    const message: string = error.response?.data?.message ?? error.message ?? '网络错误'
    return Promise.reject(new Error(message))
  },
)

// 泛型请求：返回 data 字段
export async function request<T>(config: Parameters<typeof client.request>[0]): Promise<T> {
  const resp = await client.request<ApiResponse<T>>(config)
  return resp.data.data
}

export default client
