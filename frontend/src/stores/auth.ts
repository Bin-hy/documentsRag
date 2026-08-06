// 认证状态：API Key 凭据的保存、验证与清除
import { defineStore } from 'pinia'
import { clearStoredApiKey, getStoredApiKey, setStoredApiKey } from '../api/client'
import { listKbs } from '../api/kb'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    apiKey: getStoredApiKey(),
  }),

  getters: {
    isAuthenticated: (state) => state.apiKey.length > 0,
  },

  actions: {
    /** 登录：先保存凭据使验证请求能携带 Authorization 头，失败则回滚 */
    async login(key: string) {
      setStoredApiKey(key)
      this.apiKey = key
      try {
        await listKbs() // 无效 Key 时后端返回 401，由拦截器抛错并清除凭据
      } catch (err) {
        clearStoredApiKey()
        this.apiKey = ''
        throw err
      }
    },

    logout() {
      this.apiKey = ''
      clearStoredApiKey()
    },
  },
})
