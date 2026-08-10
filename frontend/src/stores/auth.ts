// 认证状态：会话 JWT 与 API Key 双凭据的保存、验证与清除
import { defineStore } from 'pinia'
import {
  clearCredentials,
  getStoredApiKey,
  getStoredToken,
  setStoredApiKey,
  setStoredToken,
} from '../api/client'
import { exchangeTicket, getMe } from '../api/auth'
import { listKbs } from '../api/kb'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: getStoredToken(),
    apiKey: getStoredApiKey(),
  }),

  getters: {
    // 双凭据任一存在即视为已登录
    isAuthenticated: (state) => state.token.length > 0 || state.apiKey.length > 0,
  },

  actions: {
    /** API Key 登录：先保存凭据使验证请求能携带 Authorization 头，失败则回滚 */
    async loginWithAPIKey(key: string) {
      setStoredApiKey(key)
      this.apiKey = key
      try {
        await listKbs() // 无效 Key 时后端返回 401，由拦截器抛错并清除凭据
      } catch (err) {
        clearCredentials()
        this.apiKey = ''
        this.token = ''
        throw err
      }
    },

    /** OIDC/GitHub 登录：用一次性 ticket 换取会话 JWT 并保存 */
    async loginWithOIDC(ticket: string) {
      const { token } = await exchangeTicket(ticket)
      setStoredToken(token)
      this.token = token
      // 校验会话可用（可选；无效时抛错触发登录页错误提示）
      await getMe()
    },

    /** 当前身份展示信息（apikey / oidc 用户） */
    me() {
      return getMe()
    },

    logout() {
      this.token = ''
      this.apiKey = ''
      clearCredentials()
    },
  },
})
