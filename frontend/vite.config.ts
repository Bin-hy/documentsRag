import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 构建产物直接输出到 internal/webui/dist（Go embed 目录），后端单一二进制托管前端
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      // 本地开发代理到后端服务（configs/config.local.yaml 端口）
      '/api': {
        target: 'http://127.0.0.1:8085',
        changeOrigin: true,
      },
    },
  },
})
