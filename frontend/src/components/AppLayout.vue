<script setup lang="ts">
// 主布局：左侧导航 + 顶栏 + 内容区
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { ChatDotRound, Collection, Key } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

async function handleLogout() {
  try {
    await ElMessageBox.confirm('确定退出登录吗？', '退出登录', {
      confirmButtonText: '退出',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  auth.logout()
  router.push('/login')
}

// 亮/暗主题切换
function toggleTheme() {
  const html = document.documentElement
  html.classList.toggle('dark')
  localStorage.setItem('binrag_theme', html.classList.contains('dark') ? 'dark' : 'light')
}

// 初始化主题
if (localStorage.getItem('binrag_theme') === 'dark') {
  document.documentElement.classList.add('dark')
}
</script>

<template>
  <el-container class="app-layout">
    <el-aside width="200px" class="app-aside">
      <div class="app-logo">
        <span class="app-logo-icon">B</span>
        <span class="app-logo-text">BinRag</span>
      </div>
      <el-menu :default-active="route.path" router class="app-menu">
        <el-menu-item index="/chat">
          <el-icon><ChatDotRound /></el-icon>
          <span>对话问答</span>
        </el-menu-item>
        <el-menu-item index="/kb">
          <el-icon><Collection /></el-icon>
          <span>知识库</span>
        </el-menu-item>
        <el-menu-item index="/keys">
          <el-icon><Key /></el-icon>
          <span>API Key</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="app-header">
        <div class="app-header-title">知识库智能问答助手</div>
        <div class="app-header-actions">
          <el-button text @click="toggleTheme">主题</el-button>
          <el-button text type="danger" @click="handleLogout">退出登录</el-button>
        </div>
      </el-header>
      <el-main class="app-main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.app-layout {
  height: 100%;
}

.app-aside {
  background-color: var(--br-bg-card);
  border-right: 1px solid var(--br-border);
  display: flex;
  flex-direction: column;
}

.app-logo {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 18px 16px;
  font-size: 18px;
  font-weight: 600;
}

.app-logo-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background: var(--br-primary);
  color: #fff;
  font-size: 15px;
}

.app-menu {
  border-right: none;
  flex: 1;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: var(--br-bg-card);
  border-bottom: 1px solid var(--br-border);
}

.app-header-title {
  font-size: 15px;
  color: var(--br-text-secondary);
}

.app-main {
  padding: 0;
  overflow: auto;
}
</style>
