// 认证相关 API：登录 Provider 列表、ticket 换 JWT、当前身份
import { request } from './client'

export interface ProviderView {
  name: string
  type: 'oidc' | 'oauth2'
  display_name: string
}

export interface MeView {
  kind: 'apikey' | 'oidc'
  is_bootstrap?: boolean
  user_id?: string
  provider?: string
  name?: string
}

/** 已启用的登录 Provider 列表（公开接口，无需凭据） */
export function listProviders(): Promise<ProviderView[]> {
  return request<ProviderView[]>({ url: '/api/v1/auth/providers', method: 'get' })
}

/** 用一次性 ticket 换取会话 JWT（ticket 仅可成功消费一次） */
export function exchangeTicket(ticket: string): Promise<{ token: string }> {
  return request<{ token: string }>({
    url: '/api/v1/auth/exchange',
    method: 'post',
    data: { ticket },
  })
}

/** 当前身份信息（需认证） */
export function getMe(): Promise<MeView> {
  return request<MeView>({ url: '/api/v1/auth/me', method: 'get' })
}

/** 按 Provider type 拼出前端跳转的登录入口 URL */
export function providerLoginURL(p: ProviderView): string {
  if (p.type === 'oauth2') {
    return '/api/v1/auth/github/login'
  }
  return `/api/v1/auth/oidc/${encodeURIComponent(p.name)}/login`
}
