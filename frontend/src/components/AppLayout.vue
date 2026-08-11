<script setup lang="ts">
// 主布局：左侧导航 + 顶栏 + 内容区（高级化改造：品牌 logo、柔和 active 态、玻璃顶栏、图标主题切换）
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { ChatDotRound, Collection, Connection, Key, Setting, Sunny, Moon, SwitchButton } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const isDark = ref(document.documentElement.classList.contains('dark'))

// 亮/暗主题切换（html.dark 初始态由 main.ts 在挂载前设置，登录页同样生效）
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('binrag_theme', isDark.value ? 'dark' : 'light')
}

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
</script>

<template>
  <el-container class="app-layout">
    <el-aside width="216px" class="app-aside">
      <div class="app-logo">
        <span class="app-logo-mark">B</span>
        <span class="app-logo-text">
          <span class="app-logo-name">BinRag</span>
          <span class="app-logo-sub">Docs RAG</span>
        </span>
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
        <el-menu-item index="/my-mcp">
          <el-icon><Connection /></el-icon>
          <span>我的 MCP</span>
        </el-menu-item>
        <el-menu-item index="/settings">
          <el-icon><Setting /></el-icon>
          <span>系统配置</span>
        </el-menu-item>
      </el-menu>

      <div class="app-aside-foot">
        <span class="br-muted app-version">BinRag v0.1</span>
      </div>
    </el-aside>

    <el-container class="app-body">
      <el-header class="app-header">
        <div class="app-header-title">
          <span class="header-dot" />
          知识库智能问答助手
        </div>
        <div class="app-header-actions">
          <button class="theme-btn" :title="isDark ? '切换到亮色' : '切换到暗色'" @click="toggleTheme">
            <el-icon><Sunny v-if="isDark" /><Moon v-else /></el-icon>
          </button>
          <el-button text class="logout-btn" :icon="SwitchButton" @click="handleLogout">
            退出登录
          </el-button>
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

/* ---------- 侧边栏 ---------- */
.app-aside {
  background: var(--br-bg-card);
  border-right: 1px solid var(--br-border);
  display: flex;
  flex-direction: column;
}

.app-logo {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 18px 16px 14px;
}

.app-logo-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: linear-gradient(135deg, #5f6be0, #404ab0);
  color: #fff;
  font-size: 17px;
  font-weight: 700;
  box-shadow: 0 4px 10px rgba(79, 91, 213, 0.35);
}

.app-logo-text {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}

.app-logo-name {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.01em;
}

.app-logo-sub {
  font-size: 10.5px;
  color: var(--br-text-tertiary);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.app-menu {
  --el-menu-active-color: var(--br-primary);
  --el-menu-hover-bg-color: var(--br-bg-hover);
  --el-menu-item-height: 44px;
  --el-menu-item-font-size: 13.5px;
  --el-menu-base-level-padding: 14px;
  border-right: none;
  flex: 1;
  padding: 4px 10px;
}

.app-menu :deep(.el-menu-item) {
  margin: 2px 0;
  border-radius: var(--br-radius-md);
  transition:
    background-color var(--br-transition-fast),
    color var(--br-transition-fast),
    transform var(--br-transition-fast);
}

.app-menu :deep(.el-menu-item:hover) {
  transform: translateX(1px);
}

.app-menu :deep(.el-menu-item.is-active) {
  background: var(--br-primary-soft);
  font-weight: 600;
  position: relative;
}

/* active 指示条 */
.app-menu :deep(.el-menu-item.is-active::before) {
  content: '';
  position: absolute;
  left: -10px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 18px;
  border-radius: 0 3px 3px 0;
  background: var(--br-primary);
}

.app-aside-foot {
  padding: 12px 18px;
  border-top: 1px solid var(--br-border);
}

.app-version {
  font-size: 11px;
}

/* ---------- 右侧容器 ---------- */
.app-body {
  min-width: 0;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  /* 玻璃质感：半透明 + 轻模糊（header 非滚动容器，blur 安全） */
  background: color-mix(in srgb, var(--br-bg-card) 82%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--br-border);
  padding: 0 24px;
}

.app-header-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--br-text);
}

.header-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--br-primary);
  box-shadow: 0 0 0 3px var(--br-primary-soft);
}

.app-header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.theme-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-pill);
  background: var(--br-bg-card);
  color: var(--br-text-secondary);
  cursor: pointer;
  transition:
    background-color var(--br-transition-fast),
    color var(--br-transition-fast),
    border-color var(--br-transition-fast),
    transform var(--br-transition-fast);
}

.theme-btn:hover {
  background: var(--br-bg-hover);
  color: var(--br-primary);
  border-color: var(--br-primary-soft-2);
}

.theme-btn:active {
  transform: scale(0.94);
}

.logout-btn {
  color: var(--br-text-secondary);
  border-radius: var(--br-radius-pill);
  transition: color var(--br-transition-fast), background-color var(--br-transition-fast);
}

.logout-btn:hover {
  color: #f56c6c;
  background: rgba(245, 108, 108, 0.08);
}

.app-main {
  padding: 0;
  overflow: auto;
}
</style>
