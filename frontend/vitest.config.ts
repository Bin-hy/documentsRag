import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// vitest 测试配置：基于 vite 配置，jsdom 环境（store 测试需要 DOM）
export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'jsdom',
    globals: true,
  },
})
