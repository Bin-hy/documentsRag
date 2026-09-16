// 路由表与登录守卫（会话 JWT 或 API Key 任一存在即已登录）
import { createRouter, createWebHistory } from 'vue-router'
import { getStoredApiKey, getStoredToken } from '../api/client'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: () => import('../components/AppLayout.vue'),
      children: [
        { path: '', redirect: '/chat' },
        { path: 'chat', name: 'chat', component: () => import('../views/ChatView.vue') },
        { path: 'kb', name: 'kb', component: () => import('../views/KbListView.vue') },
        { path: 'kb/:id', name: 'kb-detail', component: () => import('../views/KbDetailView.vue') },
        // 评测中心（/eval/new 与 /eval/compare 必须先于 /eval/:id 注册，避免被 :id 吞掉）
        { path: 'eval', name: 'eval-list', component: () => import('../views/eval/EvalListView.vue') },
        { path: 'eval/new', name: 'eval-new', component: () => import('../views/eval/EvalNewView.vue') },
        { path: 'eval/compare', name: 'eval-compare', component: () => import('../views/eval/EvalCompareView.vue') },
        { path: 'eval/:id', name: 'eval-detail', component: () => import('../views/eval/EvalDetailView.vue') },
        { path: 'keys', name: 'keys', component: () => import('../views/ApiKeysView.vue') },
        { path: 'my-mcp', name: 'my-mcp', component: () => import('../views/MyMcpView.vue') },
        { path: 'settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
      ],
    },
  ],
})

// 登录守卫：未登录只能访问 /login
router.beforeEach((to) => {
  const authenticated = getStoredToken().length > 0 || getStoredApiKey().length > 0
  if (!to.meta.public && !authenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && authenticated) {
    return { path: '/chat' }
  }
  return true
})

export default router
