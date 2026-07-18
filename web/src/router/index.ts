import { createRouter, createWebHashHistory } from 'vue-router'

// hash 模式：本地静态服务刷新不会 404，无需服务端路由配置
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/',
      name: 'workbench',
      // 懒加载，视图随路由按需加载
      component: () => import('../views/Workbench.vue'),
    },
  ],
})

export default router
