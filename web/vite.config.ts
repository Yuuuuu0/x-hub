import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时把 /api 代理到本地主控：XHUB_DEV_TARGET=https://127.0.0.1:2096/<面板路径>
const target = process.env.XHUB_DEV_TARGET ? new URL(process.env.XHUB_DEV_TARGET) : null

export default defineConfig({
  base: './',
  plugins: [vue()],
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1500 },
  server: target
    ? {
        proxy: {
          '/api': {
            target: target.origin,
            secure: false,
            changeOrigin: true,
            rewrite: (p) => target.pathname.replace(/\/$/, '') + p,
          },
        },
      }
    : undefined,
})
