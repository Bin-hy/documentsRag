// 路由表与登录守卫
import { createRouter, createWebHistory } from 'vue-router'
import { getStoredApiKey } from '../api/client'

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
        { path: 'keys', name: 'keys', component: () => import('../views/ApiKeysView.vue') },
      ],
    },
  ],
})

// 登录守卫：未登录只能访问 /login
router.beforeEach((to) => {
  if (!to.meta.public && !getStoredApiKey()) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && getStoredApiKey()) {
    return { path: '/chat' }
  }
  return true
})

export default router
