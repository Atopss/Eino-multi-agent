import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发服务器代理 /api 到后端（默认 :8899），避免跨域并方便联调。
// 生产构建时由 nginx / 静态托管负责代理或同源部署。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    host: true,
    proxy: {
      '/api': {
        target: 'http://localhost:8899',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    rollupOptions: {
      output: {
        // 把体积大、变动少的第三方库拆成独立 vendor 包，
        // 消除「1MB Workbench chunk」告警，并让首屏缓存更稳定。
        manualChunks: {
          'vue-vendor': ['vue', 'vue-router', 'pinia'],
          'hljs-vendor': ['highlight.js'],
        },
      },
    },
  },
})
