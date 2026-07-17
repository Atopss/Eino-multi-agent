import { createRouter, createWebHashHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

// hash 模式：本地静态服务刷新不会 404，无需服务端路由配置
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/Login.vue'),
    },
    {
      path: '/',
      name: 'workbench',
      // 懒加载，视图随路由按需加载
      component: () => import('../views/Workbench.vue'),
    },
  ],
})

// 全局守卫：统一读 auth store（单一真相源，含过期校验），
// 避免与 localStorage 直接读取产生状态分歧。
router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.path !== '/login' && !auth.isAuthenticated) {
    return { path: '/login' }
  }
  if (to.path === '/login' && auth.isAuthenticated) {
    return { path: '/' }
  }
})

export default router
